package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// logDeniedRBACAction records a refused env or secret change to one key (resourceName is app/KEY).
func (h *Handler) logDeniedRBACAction(
	r *http.Request,
	email string,
	resource rbac.Resource,
	action rbac.Action,
	resourceName, details string,
	status int,
) {
	_ = h.logAudit(r, &db.AuditLog{
		UserEmail:      email,
		UserName:       r.Header.Get("X-User-Name"),
		ActionType:     audit.ActionTypeConvox,
		Action:         audit.BuildAction(resource.String(), action.String()),
		ResourceType:   resource.String(),
		Resource:       resourceName,
		Details:        details,
		IPAddress:      netutil.ClientIP(r),
		UserAgent:      r.UserAgent(),
		Status:         "denied",
		RBACDecision:   "deny",
		HTTPStatus:     status,
		ResponseTimeMs: 0,
	})
}

func (h *Handler) prepareReleaseIfNeeded(
	r *http.Request,
	w http.ResponseWriter,
	allowed bool,
	rackPath string,
	rackConfig config.RackConfig,
	userEmail string,
	start time.Time,
) ([]envutil.EnvDiff, error) {
	if !allowed || r.Method != http.MethodPost || !strings.Contains(rackPath, "/releases") {
		return nil, nil
	}

	diffs, err := h.prepareReleaseCreate(r, rackConfig, userEmail)
	if err == nil {
		return diffs, nil
	}
	if writeEnvChangeRefusal(w, err) {
		return nil, err
	}
	if errors.Is(err, db.ErrAppEnvLockBusy) {
		h.handleError(w, r, "Another env change to this app is in progress. Try again.", http.StatusConflict,
			rackConfig.Name, start)
		return nil, err
	}
	if errors.Is(err, envutil.ErrBuildReadingEnv) {
		h.handleError(w, r, err.Error(), http.StatusConflict, rackConfig.Name, start)
		return nil, err
	}
	if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
		logRackTLSMismatch("env_fetch", fpErr)
		h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackConfig.Name, start)
		return nil, err
	}
	h.handleError(w, r, err.Error(), bodyErrorStatus(err, http.StatusBadRequest), rackConfig.Name, start)
	return nil, err
}

// prepareReleaseCreate checks the env in a release create request against the caller's permissions and the
// app's protected keys, merges masked values with the current env, and rewrites the request body. Refused
// changes are returned as *envChangeDeniedError, or *protectedEnvMFAError when a protected change lacks MFA.
func (h *Handler) prepareReleaseCreate(
	r *http.Request,
	rack config.RackConfig,
	email string,
) ([]envutil.EnvDiff, error) {
	bodyBuf, vals, err := readAndParseRequestBody(r)
	if err != nil {
		return nil, err
	}

	// The rack treats a present env as the release's whole env, even when it's empty, so only an absent env
	// leaves the env alone. Without an env, parent-release would give the new release that release's env.
	if !vals.Has("env") {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		if vals.Has("parent-release") {
			return nil, &envChangeDeniedError{
				message: "parent-release needs an env: the new release would take that release's env unchecked",
			}
		}
		return nil, nil
	}
	envStr := vals.Get("env")

	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return nil, fmt.Errorf("could not infer app name from path")
	}

	posted, order, err := parsePostedEnv(envStr)
	if err != nil {
		return nil, err
	}
	keys := h.loadAppEnvKeys(app)

	if err := h.validateSecretsPermissions(r, email, keys, posted, order); err != nil {
		return nil, err
	}

	if err := h.validateProtectedKeys(r, email, keys, posted); err != nil {
		return nil, err
	}

	// Held until ProxyToRack returns, after the rack has created the release.
	if err := h.lockAppEnv(r, app); err != nil {
		return nil, err
	}
	if err := h.checkNoBuildReadingEnv(r, rack, app); err != nil {
		return nil, err
	}
	baseEnv, err := h.fetchBaseEnv(r, rack, app, bodyBuf)
	if err != nil {
		return nil, err
	}

	if err := validateChangedEntries(envStr, baseEnv); err != nil {
		return nil, err
	}

	if err := h.validateEnvPermissions(r, email, app, order); err != nil {
		return nil, err
	}

	canSecretsSet := h.callerCan(r, rbac.ResourceSecret, rbac.ActionSet)
	merged, diffs, err := h.mergeEnvAndComputeDiffs(r, email, keys, posted, order, baseEnv, canSecretsSet)
	if err != nil {
		return nil, err
	}

	newEnvString := recomposeEnvString(merged, order, baseEnv, diffs)
	vals.Set("env", newEnvString)
	newBody := []byte(vals.Encode())
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))
	return diffs, nil
}

// checkNoBuildReadingEnv refuses the env change while a build the gateway checked may still read the app's env
// (envutil.CheckNoBuildReadingEnv).
func (h *Handler) checkNoBuildReadingEnv(r *http.Request, rack config.RackConfig, app string) error {
	tlsCfg, err := h.rackTLSConfig(r.Context())
	if err != nil {
		return fmt.Errorf("failed to prepare rack TLS: %w", err)
	}
	return envutil.CheckNoBuildReadingEnv(r.Context(), h.database, rack, tlsCfg, app)
}

// readAndParseRequestBody reads a form-encoded body. Any other body is refused: the rack would parse fields (such
// as env) out of it that the gateway can't see.
func readAndParseRequestBody(r *http.Request) ([]byte, url.Values, error) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			return nil, nil, fmt.Errorf("release requests must be form-encoded, got %q", contentType)
		}
	}
	bodyBuf, err := readBufferedBody(r)
	if err != nil {
		return nil, nil, err
	}
	vals, err := url.ParseQuery(string(bodyBuf))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid form body: %w", err)
	}
	return bodyBuf, vals, nil
}

func (h *Handler) validateSecretsPermissions(
	r *http.Request,
	email string,
	keys appEnvKeys,
	posted map[string]string,
	order []string,
) error {
	canSecretsSet := h.callerCan(r, rbac.ResourceSecret, rbac.ActionSet)
	if canSecretsSet {
		return nil
	}
	offending := make([]string, 0)
	for _, k := range order {
		if keys.isSecret(k) && posted[k] != maskedSecret {
			offending = append(offending, k)
		}
	}
	if len(offending) == 0 {
		return nil
	}
	for _, key := range offending {
		resource := fmt.Sprintf("%s/%s", keys.app, key)
		h.logDeniedRBACAction(r, email, rbac.ResourceSecret, rbac.ActionSet, resource, "{}", http.StatusForbidden)
	}
	return secretsDenied(offending)
}

func (h *Handler) fetchBaseEnv(
	r *http.Request,
	rack config.RackConfig,
	app string,
	bodyBuf []byte,
) (map[string]string, error) {
	tlsCfg, err := h.rackTLSConfig(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}
	baseEnv, err := envutil.FetchLatestEnvMap(r.Context(), rack, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("env_fetch", fpErr)
			return nil, fpErr
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return nil, fmt.Errorf("failed to fetch latest env: %w", err)
	}
	return baseEnv, nil
}

func (h *Handler) validateEnvPermissions(
	r *http.Request,
	email, app string,
	order []string,
) error {
	canEnvSet := h.callerCan(r, rbac.ResourceEnv, rbac.ActionSet)
	if canEnvSet {
		return nil
	}
	for _, key := range order {
		h.logDeniedRBACAction(
			r, email, rbac.ResourceEnv, rbac.ActionSet, fmt.Sprintf("%s/%s", app, key), "{}", http.StatusForbidden,
		)
	}
	return &envChangeDeniedError{message: "You don't have permission to change environment variables: " +
		strings.Join(order, ", ")}
}

func (h *Handler) mergeEnvAndComputeDiffs(
	r *http.Request,
	email string,
	keys appEnvKeys,
	posted map[string]string,
	order []string,
	baseEnv map[string]string,
	canSecretsSet bool,
) (map[string]string, []envutil.EnvDiff, error) {
	merged := make(map[string]string)
	diffs := make([]envutil.EnvDiff, 0)

	for _, key := range order {
		val := posted[key]
		base, exists := baseEnv[key]
		isSecret := keys.isSecret(key)
		if val == maskedSecret {
			// The mask keeps the current value. Without one, the key would be created empty, past the secret and
			// protected checks (which skip masked values) and without an audit entry.
			if !exists {
				return nil, nil, fmt.Errorf("%w: %s is masked, but has no current value to keep",
					envutil.ErrMaskedSecretWithoutBase, key)
			}
			merged[key] = base
			continue
		}
		if isSecret && !canSecretsSet && val != base {
			return nil, nil, secretsDenied([]string{key})
		}
		merged[key] = val
		if val != base {
			diffs = append(diffs, envutil.EnvDiff{Key: key, OldVal: base, NewVal: val, Secret: isSecret})
		}
	}

	removed, err := removedKeyDiffs(r, keys, posted, baseEnv, merged, canSecretsSet)
	if err != nil {
		return nil, nil, err
	}
	diffs = append(diffs, removed...)

	markProtectedDiffs(keys, diffs)
	if err := h.validateProtectedDiffs(r, email, keys.app, diffs); err != nil {
		return nil, nil, err
	}

	return merged, diffs, nil
}

// removedKeyDiffs returns diffs for keys in the current env that the request leaves out, i.e. removes.
// Protected keys left out are kept (copied into merged): the CLI does not post them back. A protected key the
// request names in X-Allow-Protected-Env is removed like any other key (`env unset --allow-protected`), and
// the removal is then authorized as a protected change. Removing a secret needs secret:set.
func removedKeyDiffs(
	r *http.Request,
	keys appEnvKeys,
	posted, baseEnv, merged map[string]string,
	canSecretsSet bool,
) ([]envutil.EnvDiff, error) {
	optedIn := optedInProtectedKeys(r)
	var diffs []envutil.EnvDiff
	for key, base := range baseEnv {
		if _, ok := posted[key]; ok {
			continue
		}
		if keys.isProtected(key) && !optedIn.has(key) {
			merged[key] = base
			continue
		}
		isSecret := keys.isSecret(key)
		if isSecret && !canSecretsSet {
			return nil, secretsDenied([]string{key})
		}
		diffs = append(diffs, envutil.EnvDiff{Key: key, OldVal: base, Secret: isSecret, Removed: true})
	}
	return diffs, nil
}

func recomposeEnvString(
	merged map[string]string,
	order []string,
	baseEnv map[string]string,
	diffs []envutil.EnvDiff,
) string {
	removedKeys := make(map[string]struct{})
	for _, d := range diffs {
		if d.Removed {
			removedKeys[d.Key] = struct{}{}
		}
	}

	var b strings.Builder
	used := map[string]struct{}{}
	for i, k := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(merged[k])
		used[k] = struct{}{}
	}
	for k, v := range baseEnv {
		if _, ok := used[k]; ok {
			continue
		}
		if _, removed := removedKeys[k]; removed {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	return b.String()
}

// logEnvDiffs writes one audit entry per env change. Its values are redacted unless the app's
// audit_visible_env_vars lists the key and it is neither secret nor protected.
func (h *Handler) logEnvDiffs(r *http.Request, email, _ string, diffs []envutil.EnvDiff) {
	if len(diffs) == 0 {
		return
	}
	userName := r.Header.Get("X-User-Name")
	app := extractAppFromPath(r.URL.Path)
	visible := h.auditVisibleKeys(app)
	for _, d := range diffs {
		action, rtype := audit.EnvChangeAction(d)
		_ = h.logAudit(r, &db.AuditLog{
			UserEmail:      email,
			UserName:       userName,
			ActionType:     audit.ActionTypeConvox,
			Action:         action,
			ResourceType:   rtype,
			Resource:       fmt.Sprintf("%s/%s", app, d.Key),
			Details:        visible.AuditDetails(d),
			IPAddress:      netutil.ClientIP(r),
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: 0,
		})
	}
}
