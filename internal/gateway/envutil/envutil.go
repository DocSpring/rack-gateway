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
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
)

const MaskedSecret = "********************"

var (
	ErrSecretPermission         = errors.New("secrets:set permission required")
	ErrProtectedEnvModification = errors.New("protected env var modification denied")
	ErrMaskedSecretWithoutBase  = errors.New("masked secret provided without existing value")
)

// EnvDiff describes a change applied to an environment variable.
type EnvDiff struct {
	Key    string
	OldVal string
	NewVal string
	Secret bool
}

// MergeOptions controls how MergeEnv applies updates.
type MergeOptions struct {
	AllowSecretUpdates bool
	IsSecretKey        func(string) bool
	IsProtectedKey     func(string) bool
}

// MergeEnv applies the supplied set/remove operations to the base environment map
// and returns the merged environment alongside a list of diffs suitable for auditing.
func MergeEnv(base map[string]string, set map[string]string, remove []string, opts MergeOptions) (map[string]string, []EnvDiff, error) {
	merged := make(map[string]string, len(base))
	removedOld := make(map[string]string, len(remove))
	for k, v := range base {
		merged[k] = v
	}

	diffs := make([]EnvDiff, 0)

	// Process removals first so that set operations can re-add if needed.
	for _, rawKey := range remove {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		if opts.IsProtectedKey != nil && opts.IsProtectedKey(key) {
			return nil, nil, ErrProtectedEnvModification
		}
		oldVal, ok := merged[key]
		if !ok {
			continue
		}
		secret := opts.IsSecretKey != nil && opts.IsSecretKey(key)
		if secret && !opts.AllowSecretUpdates {
			return nil, nil, ErrSecretPermission
		}
		diffs = append(diffs, EnvDiff{Key: key, OldVal: oldVal, NewVal: "", Secret: secret})
		removedOld[key] = oldVal
		delete(merged, key)
	}

	if len(set) == 0 {
		return merged, diffs, nil
	}

	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}

		value := set[rawKey]

		if opts.IsProtectedKey != nil && opts.IsProtectedKey(key) {
			// Modifying protected keys is prohibited regardless of value changes.
			if _, exists := merged[key]; exists {
				if value != merged[key] {
					return nil, nil, ErrProtectedEnvModification
				}
				continue
			}
			// Existing value might have been removed earlier; treat any set as modification.
			if _, existed := removedOld[key]; existed {
				if value != removedOld[key] {
					return nil, nil, ErrProtectedEnvModification
				}
				// Setting back to original value after removal should be no-op.
				merged[key] = value
				continue
			}
			return nil, nil, ErrProtectedEnvModification
		}

		secret := opts.IsSecretKey != nil && opts.IsSecretKey(key)

		oldVal, hadOld := base[key]
		if !hadOld {
			if prev, ok := removedOld[key]; ok {
				oldVal = prev
				hadOld = true
			}
		}

		if secret && value == MaskedSecret {
			if !hadOld {
				return nil, nil, ErrMaskedSecretWithoutBase
			}
			// Treat masked value as a no-op (preserve underlying secret).
			merged[key] = oldVal
			continue
		}

		if secret && !opts.AllowSecretUpdates {
			if !hadOld || value != oldVal {
				return nil, nil, ErrSecretPermission
			}
		}

		merged[key] = value
		if !hadOld || value != oldVal {
			diffs = append(diffs, EnvDiff{Key: key, OldVal: oldVal, NewVal: value, Secret: secret})
		}
	}

	return merged, diffs, nil
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
func CreateReleaseWithEnv(ctx context.Context, rack config.RackConfig, tlsConfig *tls.Config, app, env string) (string, error) {
	base := strings.TrimRight(rack.URL, "/")
	if base == "" {
		return "", fmt.Errorf("rack URL is empty")
	}

	client := httpclient.NewRackClient(30*time.Second, tlsConfig)
	username := strings.TrimSpace(rack.Username)
	if username == "" {
		username = "convox"
	}
	vals := url.Values{}
	vals.Set("env", env)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/apps/%s/releases", base, app), strings.NewReader(vals.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build release request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, rack.APIKey)))
	req.Header.Set("Authorization", authHeader)

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

// IsSecretKey returns true if key looks secret-like (by suffix/pattern) or is in explicit list.
func IsSecretKey(key string, explicit []string) bool {
	upper := strings.ToUpper(key)
	if strings.Contains(upper, "SECRET") || strings.Contains(upper, "TOKEN") {
		return true
	}
	if strings.HasSuffix(upper, "_KEY") || strings.HasSuffix(upper, "_KEY_ID") {
		return true
	}
	for _, k := range explicit {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}
	return false
}

// FetchLatestEnvMap pulls the latest release then returns its env as a map.
func FetchLatestEnvMap(rack config.RackConfig, app string, tlsConfig *tls.Config) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	// Disable TLS verification for Convox API (internal/self-signed certs)
	client := httpclient.NewRackClient(10*time.Second, tlsConfig)
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))
	// List releases
	req1, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/apps/%s/releases?limit=1", base, app), nil)
	req1.Header.Set("Authorization", authHeader)
	resp1, err := client.Do(req1)
	if err != nil {
		return nil, err
	}
	defer resp1.Body.Close() //nolint:errcheck // response cleanup
	var list []map[string]interface{}
	if err := json.NewDecoder(resp1.Body).Decode(&list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return map[string]string{}, nil
	}
	id, _ := list[0]["id"].(string)
	if id == "" {
		return map[string]string{}, nil
	}
	// Get release
	req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/apps/%s/releases/%s", base, app, id), nil)
	req2.Header.Set("Authorization", authHeader)
	resp2, err := client.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close() //nolint:errcheck // response cleanup
	var rel map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&rel); err != nil {
		return nil, err
	}
	envStr, _ := rel["env"].(string)
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
	return out, nil
}
