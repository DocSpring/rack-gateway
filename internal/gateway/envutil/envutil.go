package envutil

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
)

// MaskedSecret is the placeholder string displayed in place of actual secret values.
const MaskedSecret = "********************"

// AllowProtectedHeader is the request header in which `env set/unset --allow-protected` names the protected
// env vars the command changes, comma-separated. The gateway only accepts it from callers holding
// convox:env:set_protected who verify MFA on that request, and never forwards it to the rack.
const AllowProtectedHeader = "X-Allow-Protected-Env"

var (
	// ErrSecretPermission is returned when a secret update is attempted without proper permissions.
	ErrSecretPermission = errors.New("secrets:set permission required")
	// ErrProtectedEnvModification is returned when attempting to modify a protected environment variable.
	ErrProtectedEnvModification = errors.New("protected env var modification denied")
	// ErrMaskedSecretWithoutBase is returned when a masked secret value is provided without an existing
	// value to preserve.
	ErrMaskedSecretWithoutBase = errors.New("masked secret provided without existing value")
	// ErrInvalidEnvEntry is returned for keys that aren't valid env var names, or values that aren't UTF-8 or
	// contain control characters other than tab. The rack API takes env as newline-separated KEY=VALUE lines, so
	// a line break in a value would inject extra variables, and `convox env` prints values to other users'
	// terminals, where an escape sequence would run.
	ErrInvalidEnvEntry = errors.New("invalid environment variable name or value")
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidName reports whether name is a valid env var name: letters, digits and underscores, not starting with a
// digit.
func ValidName(name string) bool {
	return envKeyPattern.MatchString(name)
}

// maxShownKeyBytes is how much of a key an error message shows: the message goes back to the client and into the
// logs, and a key can be most of a posted env.
const maxShownKeyBytes = 40

// ValidateEnvEntry checks that key is a valid env var name and value is UTF-8 without control characters other
// than tab. An invalid name isn't shown in the error (it may be a line of a multi-line secret value); a value error
// names its key, cut to 40 bytes.
func ValidateEnvEntry(key, value string) error {
	if !ValidName(key) {
		return fmt.Errorf("%w: names must be letters, digits and underscores, not starting with a digit",
			ErrInvalidEnvEntry)
	}
	if HasControlCharacter(value) {
		return fmt.Errorf("%w: the value of %s contains a control character or isn't UTF-8",
			ErrInvalidEnvEntry, shownKey(key))
	}
	return nil
}

// HasControlCharacter reports whether s isn't UTF-8 or contains a control character (C0, DEL or C1) other than tab.
func HasControlCharacter(s string) bool {
	return !utf8.ValidString(s) || strings.IndexFunc(s, isControlRune) >= 0
}

// isControlRune reports a control character (C0, DEL or C1) other than tab.
func isControlRune(r rune) bool {
	return r != '\t' && unicode.IsControl(r)
}

// shownKey is a valid env var name as an error message shows it, cut to maxShownKeyBytes.
func shownKey(key string) string {
	if len(key) <= maxShownKeyBytes {
		return key
	}
	return key[:maxShownKeyBytes] + "…"
}

// EnvDiff describes a change applied to an environment variable.
type EnvDiff struct {
	Key    string
	OldVal string
	NewVal string
	Secret bool
	// Protected marks a change to one of the app's protected env vars. Its values are never logged.
	Protected bool
	// Removed marks the key's removal. A key set to an empty value is still set.
	Removed bool
}

// MergeOptions controls how MergeEnv applies updates.
type MergeOptions struct {
	AllowSecretUpdates bool
	IsSecretKey        func(string) bool
	IsProtectedKey     func(string) bool
}

// MergeEnv applies the supplied set/remove operations to the base environment map
// and returns the merged environment alongside a list of diffs suitable for auditing.
func MergeEnv(
	base map[string]string,
	set map[string]string,
	remove []string,
	opts MergeOptions,
) (map[string]string, []EnvDiff, error) {
	merged := copyMap(base)
	removedOld := make(map[string]string, len(remove))
	diffs := make([]EnvDiff, 0)

	// Process removals first so that set operations can re-add if needed.
	newDiffs, err := processRemovals(remove, merged, removedOld, opts)
	if err != nil {
		return nil, nil, err
	}
	diffs = append(diffs, newDiffs...)

	if len(set) == 0 {
		return merged, diffs, nil
	}

	// Process set operations
	newDiffs, err = processSetOperations(set, base, merged, removedOld, opts)
	if err != nil {
		return nil, nil, err
	}
	diffs = append(diffs, newDiffs...)

	return merged, diffs, nil
}

func copyMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func processRemovals(
	remove []string,
	merged map[string]string,
	removedOld map[string]string,
	opts MergeOptions,
) ([]EnvDiff, error) {
	diffs := make([]EnvDiff, 0)
	for _, rawKey := range remove {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		if err := ValidateEnvEntry(key, ""); err != nil {
			return nil, err
		}
		if opts.IsProtectedKey != nil && opts.IsProtectedKey(key) {
			return nil, ErrProtectedEnvModification
		}
		oldVal, ok := merged[key]
		if !ok {
			continue
		}
		secret := opts.IsSecretKey != nil && opts.IsSecretKey(key)
		if secret && !opts.AllowSecretUpdates {
			return nil, ErrSecretPermission
		}
		diffs = append(diffs, EnvDiff{Key: key, OldVal: oldVal, NewVal: "", Secret: secret, Removed: true})
		removedOld[key] = oldVal
		delete(merged, key)
	}
	return diffs, nil
}

func processSetOperations(
	set map[string]string,
	base map[string]string,
	merged map[string]string,
	removedOld map[string]string,
	opts MergeOptions,
) ([]EnvDiff, error) {
	keys := getSortedKeys(set)
	diffs := make([]EnvDiff, 0)

	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}

		diff, err := processSetOperation(key, set[rawKey], base, merged, removedOld, opts)
		if err != nil {
			return nil, err
		}
		if diff != nil {
			diffs = append(diffs, *diff)
		}
	}

	return diffs, nil
}

func processSetOperation(
	key string,
	value string,
	base map[string]string,
	merged map[string]string,
	removedOld map[string]string,
	opts MergeOptions,
) (*EnvDiff, error) {
	if err := ValidateEnvEntry(key, value); err != nil {
		return nil, err
	}
	// The web editor has no opt-in for protected keys, so any value is refused, the current one included: accepting
	// an unchanged value would tell a caller who can't read it whether a guess was right.
	if opts.IsProtectedKey != nil && opts.IsProtectedKey(key) {
		return nil, ErrProtectedEnvModification
	}

	secret := opts.IsSecretKey != nil && opts.IsSecretKey(key)
	oldVal, hadOld := getOldValue(key, base, removedOld)

	// The mask keeps the current value: it's what the editor shows for a secret, and for a value with credentials in
	// it (MaskedForReaders), which isn't a secret by name.
	if value == MaskedSecret {
		if !hadOld {
			return nil, ErrMaskedSecretWithoutBase
		}
		merged[key] = oldVal
		return nil, nil
	}

	// Without secrets:set, the mask (keep the current value) is the only value a secret takes, for the same
	// reason: accepting the current value would confirm a guess.
	if secret && !opts.AllowSecretUpdates {
		return nil, ErrSecretPermission
	}

	merged[key] = value
	if !hadOld || value != oldVal {
		return &EnvDiff{Key: key, OldVal: oldVal, NewVal: value, Secret: secret}, nil
	}

	return nil, nil
}

func getSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func getOldValue(key string, base map[string]string, removedOld map[string]string) (string, bool) {
	if oldVal, hadOld := base[key]; hadOld {
		return oldVal, true
	}
	if prev, ok := removedOld[key]; ok {
		return prev, true
	}
	return "", false
}

// BuildEnvString converts an env map into the newline-separated KEY=VALUE string expected by the rack API.
func BuildEnvString(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(env[k])
	}
	return b.String()
}

// CreateReleaseWithEnv posts a new release to the rack with the supplied env payload and returns the release id.
func CreateReleaseWithEnv(
	ctx context.Context,
	rack config.RackConfig,
	tlsConfig *tls.Config,
	app, env string,
) (string, error) {
	base := strings.TrimRight(rack.URL, "/")
	if base == "" {
		return "", fmt.Errorf("rack URL is empty")
	}

	client := httpclient.NewRackClient(30*time.Second, tlsConfig)
	vals := url.Values{}
	vals.Set("env", env)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/apps/%s/releases", base, url.PathEscape(app)),
		strings.NewReader(vals.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("failed to build release request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", rackAuthHeader(rack))

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("release request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if strings.TrimSpace(body.Error) == "" {
			body.Error = resp.Status
		}
		return "", fmt.Errorf("rack release create failed: %s", strings.TrimSpace(body.Error))
	}

	var release struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode release response: %w", err)
	}
	return release.ID, nil
}

// FetchLatestEnvMap pulls the latest release then returns its env as a map. An app without a release has an empty
// env. Anything else the rack answers that isn't a release (an error, a release without an ID or an env) is an
// error, never an empty env: callers write the whole env back as a new release, so an empty base would drop every
// variable the change didn't name.
func FetchLatestEnvMap(
	ctx context.Context,
	rack config.RackConfig,
	app string,
	tlsConfig *tls.Config,
) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	client := httpclient.NewRackClient(10*time.Second, tlsConfig)
	authHeader := rackAuthHeader(rack)
	var list []struct {
		ID string `json:"id"`
	}
	listURL := fmt.Sprintf("%s/apps/%s/releases?limit=1", base, url.PathEscape(app))
	if err := getRackJSON(ctx, client, listURL, authHeader, &list); err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}
	if len(list) == 0 {
		return map[string]string{}, nil
	}
	if list[0].ID == "" {
		return nil, errors.New("the rack's latest release has no ID")
	}
	var rel struct {
		Env *string `json:"env"`
	}
	releaseURL := fmt.Sprintf("%s/apps/%s/releases/%s", base, url.PathEscape(app), url.PathEscape(list[0].ID))
	if err := getRackJSON(ctx, client, releaseURL, authHeader, &rel); err != nil {
		return nil, fmt.Errorf("failed to get release %s: %w", list[0].ID, err)
	}
	if rel.Env == nil {
		return nil, fmt.Errorf("release %s has no env", list[0].ID)
	}
	return parseEnvLines(*rel.Env), nil
}

// rackAuthHeader is the Authorization header for the rack's API.
func rackAuthHeader(rack config.RackConfig) string {
	username := strings.TrimSpace(rack.Username)
	if username == "" {
		username = "convox"
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+rack.APIKey))
}

// getRackJSON GETs a rack API URL and decodes its JSON answer into out. A non-2xx answer is an error.
func getRackJSON(ctx context.Context, client *http.Client, rackURL, authHeader string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rackURL, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("the rack answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// parseEnvLines parses a release's env, newline-separated KEY=VALUE lines.
func parseEnvLines(envStr string) map[string]string {
	out := map[string]string{}
	for _, ln := range strings.Split(envStr, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		k := parts[0]
		v := ""
		if len(parts) == 2 {
			v = parts[1]
		}
		out[k] = v
	}
	return out
}
