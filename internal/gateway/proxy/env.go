package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
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
		IPAddress:      clientIPFromRequest(r),
		UserAgent:      r.UserAgent(),
		Status:         "denied",
		RBACDecision:   "deny",
		HTTPStatus:     http.StatusForbidden,
		ResponseTimeMs: 0,
	})
}

func (h *Handler) checkEnvSetPermissions(r *http.Request, email string) bool {
	// Extract keys from known headers
	keys := h.extractEnvKeysFromHeaders(r.Header)
	if len(keys) == 0 {
		// No explicit env changes detected; allow
		return true
	}
	// Require env:set for any env changes
	canEnvSet, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionSet)
	if !canEnvSet {
		return false
	}
	// For secret keys, require secrets:set
	canSecretsSet, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionSet)
	if !canSecretsSet {
		for _, k := range keys {
			if h.isSecretKey(k) {
				return false
			}
		}
	}
	return true
}

func (h *Handler) extractEnvKeysFromHeaders(hdr http.Header) []string {
	keys := make([]string, 0)
	for name, vals := range hdr {
		ln := strings.ToLower(name)
		if ln == "env" || ln == "environment" || ln == "release-env" {
			for _, v := range vals {
				for _, line := range strings.Split(v, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.SplitN(line, "=", 2)
					k := strings.TrimSpace(parts[0])
					if k != "" {
						keys = append(keys, k)
					}
				}
			}
		}
	}
	return keys
}

func (h *Handler) prepareReleaseCreate(r *http.Request, rack config.RackConfig, email string) (bool, []envutil.EnvDiff, error) {
	// Read and buffer original body
	var bodyBuf []byte
	if r.Body != nil {
		var err error
		bodyBuf, err = io.ReadAll(r.Body)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		if err := r.Body.Close(); err != nil {
			return false, nil, fmt.Errorf("failed to close request body: %w", err)
		}
	}
	// Parse form
	vals, err := url.ParseQuery(string(bodyBuf))
	if err != nil {
		return false, nil, fmt.Errorf("invalid form body: %w", err)
	}
	envStr := vals.Get("env")
	if envStr == "" {
		// no env set attempt => allow
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return true, nil, nil
	}

	// Get app name from path /apps/{app}/releases
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("could not infer app name from path")
	}

	// Parse posted env into ordered keys
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

	// If attempting to set secret values without permission, deny early (no need to fetch base)
	canSecretsSet, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionSet)
	if !canSecretsSet {
		offending := make([]string, 0)
		for _, k := range order {
			if h.isSecretKey(k) && posted[k] != maskedSecret {
				offending = append(offending, k)
			}
		}
		if len(offending) > 0 {
			// Log denied secrets.set per offending key for audit clarity
			userName := r.Header.Get("X-User-Name")
			for _, key := range offending {
				h.logDeniedRBACAction(
					r,
					email,
					userName,
					rbac.ResourceSecret,
					rbac.ActionSet,
					"secret",
					fmt.Sprintf("%s/%s", app, key),
					"{}",
				)
			}
			return false, nil, nil
		}
	}

	// If posting any protected key explicitly, deny immediately (no change to protected keys allowed)
	for k := range posted {
		if h.isProtectedKeyForApp(k, app) {
			userName := r.Header.Get("X-User-Name")
			h.logDeniedRBACAction(
				r,
				email,
				userName,
				rbac.ResourceEnv,
				rbac.ActionSet,
				"env",
				fmt.Sprintf("%s/%s", app, k),
				"{\"error\":\"protected key change denied\"}",
			)
			return false, nil, nil
		}
	}

	// Fetch latest env map from rack (needed to fill back masked values and compute diffs)
	tlsCfg, err := h.rackTLSConfig(r.Context())
	if err != nil {
		return false, nil, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}
	baseEnv, err := envutil.FetchLatestEnvMap(rack, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("env_fetch", fpErr)
			return false, nil, fpErr
		}
		// If fetch fails, fall back to submitted body without rewrite
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("failed to fetch latest env: %w", err)
	}

	// Permissions
	canEnvSet, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionSet)
	canSecretsSet, _ = h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceSecret, rbac.ActionSet)
	if !canEnvSet {
		// Log denied env.set entries for submitted keys
		userName := r.Header.Get("X-User-Name")
		for _, key := range order {
			h.logDeniedRBACAction(
				r,
				email,
				userName,
				rbac.ResourceEnv,
				rbac.ActionSet,
				"env",
				fmt.Sprintf("%s/%s", app, key),
				"{}",
			)
		}
		return false, nil, nil
	}

	// Do not require protected keys to be present in the payload; we will carry them over from base below.

	// Merge masked values and compute diffs
	merged := make(map[string]string)
	diffs := make([]envutil.EnvDiff, 0)
	removed := make(map[string]envutil.EnvDiff)
	for _, key := range order {
		val := posted[key]
		base := baseEnv[key]
		isSecret := h.isSecretKey(key)
		// If masked, keep base value
		if val == maskedSecret {
			merged[key] = base
			continue
		}
		// If changing a secret without permission, deny
		if isSecret && !canSecretsSet && val != base {
			return false, nil, nil
		}
		merged[key] = val
		if val != base {
			diffs = append(diffs, envutil.EnvDiff{Key: key, OldVal: base, NewVal: val, Secret: isSecret})
		}
	}
	for key, base := range baseEnv {
		if _, ok := posted[key]; ok {
			continue
		}
		removed[key] = envutil.EnvDiff{Key: key, OldVal: base, NewVal: "", Secret: h.isSecretKey(key)}
	}
	if len(removed) > 0 {
		for _, diff := range removed {
			diffs = append(diffs, diff)
		}
	}

	// Deny any modifications to protected env vars
	for _, d := range diffs {
		if h.isProtectedKeyForApp(d.Key, app) {
			// Log denied change for protected key
			userName := r.Header.Get("X-User-Name")
			_ = h.logAudit(r, &db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()),
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, d.Key),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      clientIPFromRequest(r),
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
	}

	// Recompose env string preserving submitted order and appending any base-only keys
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
	// Append remaining base keys to ensure full env for release
	for k, v := range baseEnv {
		if _, ok := used[k]; ok {
			continue
		}
		if _, removed := removed[k]; removed {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	vals.Set("env", b.String())
	newBody := []byte(vals.Encode())
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	// Ensure Content-Length is ignored downstream (we strip it in response), request side proxy will re-create
	r.ContentLength = int64(len(newBody))
	return true, diffs, nil
}

func (h *Handler) logEnvDiffs(r *http.Request, email, rack string, diffs []envutil.EnvDiff) {
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
			IPAddress:      clientIPFromRequest(r),
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
