package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *Handler) logDeniedRBACAction(
	r *http.Request,
	email,
	userName string,
	resource rbac.Resource,
	action rbac.Action,
	resourceType,
	resourceName,
	details string,
) {
	_ = h.logAudit(r, &db.AuditLog{
		UserEmail:      email,
		UserName:       userName,
		ActionType:     "convox",
		Action:         audit.BuildAction(resource.String(), action.String()),
		ResourceType:   resourceType,
		Resource:       resourceName,
		Details:        details,
		IPAddress:      netutil.ClientIP(r),
		UserAgent:      r.UserAgent(),
		Status:         "denied",
		RBACDecision:   "deny",
		HTTPStatus:     http.StatusForbidden,
		ResponseTimeMs: 0,
	})
}

// prepareReleaseCreate checks the env in a release create request against the caller's permissions and the
// app's protected keys, merges masked values with the current env, and rewrites the request body. Refused
// changes are returned as *envChangeDeniedError.
func (h *Handler) prepareReleaseCreate(
	r *http.Request,
	rack config.RackConfig,
	email string,
) ([]envutil.EnvDiff, error) {
	bodyBuf, vals, err := readAndParseRequestBody(r)
	if err != nil {
		return nil, err
	}

	envStr := vals.Get("env")
	if envStr == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return nil, nil
	}

	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return nil, fmt.Errorf("could not infer app name from path")
	}

	posted, order := parsePostedEnv(envStr)

	if err := h.validateSecretsPermissions(r, email, app, posted, order); err != nil {
		return nil, err
	}

	if err := h.validateProtectedKeys(r, email, app, posted); err != nil {
		return nil, err
	}

	baseEnv, err := h.fetchBaseEnv(r, rack, app, bodyBuf)
	if err != nil {
		return nil, err
	}

	if err := h.validateEnvPermissions(r, email, app, order); err != nil {
		return nil, err
	}

	canSecretsSet := h.callerCan(r, rbac.ResourceSecret, rbac.ActionSet)
	merged, diffs, err := h.mergeEnvAndComputeDiffs(r, email, app, posted, order, baseEnv, canSecretsSet)
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

func readAndParseRequestBody(r *http.Request) ([]byte, url.Values, error) {
	var bodyBuf []byte
	if r.Body != nil {
		var err error
		bodyBuf, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		if err := r.Body.Close(); err != nil {
			return nil, nil, fmt.Errorf("failed to close request body: %w", err)
		}
	}
	vals, err := url.ParseQuery(string(bodyBuf))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid form body: %w", err)
	}
	return bodyBuf, vals, nil
}

func parsePostedEnv(envStr string) (map[string]string, []string) {
	postedLines := strings.Split(envStr, "\n")
	posted := make(map[string]string)
	order := make([]string, 0, len(postedLines))
	for _, ln := range postedLines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := ""
		if len(parts) == 2 {
			val = parts[1]
		}
		if key == "" {
			continue
		}
		if _, seen := posted[key]; !seen {
			order = append(order, key)
		}
		posted[key] = val
	}
	return posted, order
}

func (h *Handler) validateSecretsPermissions(
	r *http.Request,
	email, app string,
	posted map[string]string,
	order []string,
) error {
	canSecretsSet := h.callerCan(r, rbac.ResourceSecret, rbac.ActionSet)
	if canSecretsSet {
		return nil
	}
	offending := make([]string, 0)
	for _, k := range order {
		if h.isSecretKey(k) && posted[k] != maskedSecret {
			offending = append(offending, k)
		}
	}
	if len(offending) == 0 {
		return nil
	}
	userName := r.Header.Get("X-User-Name")
	for _, key := range offending {
		h.logDeniedRBACAction(
			r, email, userName,
			rbac.ResourceSecret, rbac.ActionSet,
			"secret", fmt.Sprintf("%s/%s", app, key), "{}",
		)
	}
	return secretsDenied(offending)
}

func (h *Handler) validateProtectedKeys(
	r *http.Request,
	email, app string,
	posted map[string]string,
) error {
	var changed []string
	for k, v := range posted {
		if h.isProtectedKeyForApp(k, app) && v != maskedSecret {
			changed = append(changed, k)
		}
	}
	return h.denyProtectedKeys(r, email, app, changed)
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
	baseEnv, err := envutil.FetchLatestEnvMap(rack, app, tlsCfg)
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
	userName := r.Header.Get("X-User-Name")
	for _, key := range order {
		h.logDeniedRBACAction(
			r, email, userName,
			rbac.ResourceEnv, rbac.ActionSet,
			"env", fmt.Sprintf("%s/%s", app, key), "{}",
		)
	}
	return &envChangeDeniedError{message: "You don't have permission to change environment variables: " +
		strings.Join(order, ", ")}
}

func (h *Handler) mergeEnvAndComputeDiffs(
	r *http.Request,
	email, app string,
	posted map[string]string,
	order []string,
	baseEnv map[string]string,
	canSecretsSet bool,
) (map[string]string, []envutil.EnvDiff, error) {
	merged := make(map[string]string)
	diffs := make([]envutil.EnvDiff, 0)

	for _, key := range order {
		val := posted[key]
		base := baseEnv[key]
		isSecret := h.isSecretKey(key)
		if val == maskedSecret {
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

	removed, err := h.removedKeyDiffs(app, posted, baseEnv, merged, canSecretsSet)
	if err != nil {
		return nil, nil, err
	}
	diffs = append(diffs, removed...)

	if err := h.validateProtectedDiffs(r, email, app, diffs); err != nil {
		return nil, nil, err
	}

	return merged, diffs, nil
}

// removedKeyDiffs returns diffs for keys in the current env that the request leaves out, i.e. removes.
// Protected keys left out are kept (copied into merged): the CLI does not post them back. Removing a secret
// needs secret:set.
func (h *Handler) removedKeyDiffs(
	app string,
	posted, baseEnv, merged map[string]string,
	canSecretsSet bool,
) ([]envutil.EnvDiff, error) {
	var diffs []envutil.EnvDiff
	for key, base := range baseEnv {
		if _, ok := posted[key]; ok {
			continue
		}
		if h.isProtectedKeyForApp(key, app) {
			merged[key] = base
			continue
		}
		isSecret := h.isSecretKey(key)
		if isSecret && !canSecretsSet {
			return nil, secretsDenied([]string{key})
		}
		diffs = append(diffs, envutil.EnvDiff{Key: key, OldVal: base, NewVal: "", Secret: isSecret})
	}
	return diffs, nil
}

func (h *Handler) validateProtectedDiffs(
	r *http.Request,
	email, app string,
	diffs []envutil.EnvDiff,
) error {
	var changed []string
	for _, d := range diffs {
		if h.isProtectedKeyForApp(d.Key, app) {
			changed = append(changed, d.Key)
		}
	}
	return h.denyProtectedKeys(r, email, app, changed)
}

// denyProtectedKeys audits and refuses changes to the app's protected keys. It returns nil when keys is empty.
func (h *Handler) denyProtectedKeys(r *http.Request, email, app string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	userName := r.Header.Get("X-User-Name")
	for _, key := range keys {
		h.logDeniedRBACAction(
			r, email, userName,
			rbac.ResourceEnv, rbac.ActionSet,
			"env", fmt.Sprintf("%s/%s", app, key),
			"{\"error\":\"protected key change denied\"}",
		)
	}
	return protectedKeysDenied(app, keys)
}

func recomposeEnvString(
	merged map[string]string,
	order []string,
	baseEnv map[string]string,
	diffs []envutil.EnvDiff,
) string {
	removedKeys := make(map[string]struct{})
	for _, d := range diffs {
		if d.NewVal == "" {
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

func (h *Handler) logEnvDiffs(r *http.Request, email, _ string, diffs []envutil.EnvDiff) {
	if len(diffs) == 0 {
		return
	}
	userName := r.Header.Get("X-User-Name")
	app := extractAppFromPath(r.URL.Path)
	for _, d := range diffs {
		// Mask only secret values in audit details
		oldVal := d.OldVal
		newVal := d.NewVal
		if d.Secret {
			oldVal = "[REDACTED]"
			newVal = "[REDACTED]"
		}
		details := fmt.Sprintf("{\"old\":\"%s\",\"new\":\"%s\"}", escapeJSONString(oldVal), escapeJSONString(newVal))
		action := audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String())
		rtype := "env"
		if d.Secret {
			action = audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionSet.String())
			rtype = "secret"
		}
		if strings.TrimSpace(d.NewVal) == "" {
			if d.Secret {
				action = audit.BuildAction(rbac.ResourceSecret.String(), audit.ActionVerbUnset)
			} else {
				action = audit.BuildAction(rbac.ResourceEnv.String(), audit.ActionVerbUnset)
			}
		}
		_ = h.logAudit(r, &db.AuditLog{
			UserEmail:      email,
			UserName:       userName,
			ActionType:     "convox",
			Action:         action,
			ResourceType:   rtype,
			Resource:       fmt.Sprintf("%s/%s", app, d.Key),
			Details:        details,
			IPAddress:      netutil.ClientIP(r),
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: 0,
		})
	}
}

func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
