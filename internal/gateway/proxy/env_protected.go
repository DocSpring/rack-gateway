package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Protected env vars (an app's protected_env_vars setting) can't be changed by accident. The only way to change or
// remove one without unprotecting it is an explicit opt-in: the request names the key in X-Allow-Protected-Env
// (`env set/unset --allow-protected`), the caller is a user holding convox:env:set_protected (admins), and MFA is
// verified on that request, whatever the session's step-up state.

// protectedKeySet holds env var names upper-cased, matching how isProtectedKeyForApp compares keys.
type protectedKeySet map[string]struct{}

func (s protectedKeySet) has(key string) bool {
	_, ok := s[strings.ToUpper(strings.TrimSpace(key))]
	return ok
}

// optedInProtectedKeys returns the keys the request names in its X-Allow-Protected-Env header.
func optedInProtectedKeys(r *http.Request) protectedKeySet {
	keys := protectedKeySet{}
	for _, value := range r.Header.Values(envutil.AllowProtectedHeader) {
		for _, key := range strings.Split(value, ",") {
			if key = strings.ToUpper(strings.TrimSpace(key)); key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	return keys
}

// validateProtectedKeys authorizes the posted values for protected keys before the current env is fetched.
// The CLI posts masked values back for protected keys it isn't changing, so only unmasked values count.
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
	return h.authorizeProtectedKeyChanges(r, email, app, changed)
}

// markProtectedDiffs flags the diffs that change or remove one of the app's protected keys.
func (h *Handler) markProtectedDiffs(app string, diffs []envutil.EnvDiff) {
	for i := range diffs {
		diffs[i].Protected = h.isProtectedKeyForApp(diffs[i].Key, app)
	}
}

// validateProtectedDiffs authorizes the protected changes in the merged diffs, including removals.
func (h *Handler) validateProtectedDiffs(
	r *http.Request,
	email, app string,
	diffs []envutil.EnvDiff,
) error {
	var changed []string
	for _, d := range diffs {
		if d.Protected {
			changed = append(changed, d.Key)
		}
	}
	return h.authorizeProtectedKeyChanges(r, email, app, changed)
}

// authorizeProtectedKeyChanges returns nil when keys is empty or the request may change all of them. Keys the
// request didn't name in X-Allow-Protected-Env are refused as they always were; otherwise the caller and MFA
// are checked. Every refusal is audited once per key.
func (h *Handler) authorizeProtectedKeyChanges(r *http.Request, email, app string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)

	optedIn := optedInProtectedKeys(r)
	var notOptedIn []string
	for _, key := range keys {
		if !optedIn.has(key) {
			notOptedIn = append(notOptedIn, key)
		}
	}
	if len(notOptedIn) > 0 {
		h.auditProtectedRefusal(
			r, email, app, notOptedIn, rbac.ActionSet, "protected key change denied", http.StatusForbidden,
		)
		return protectedKeysDenied(app, notOptedIn)
	}

	err := h.checkProtectedChangeCaller(r, strings.Join(keys, ", "))
	if err == nil {
		return nil
	}
	status := http.StatusForbidden
	var mfaErr *protectedEnvMFAError
	if errors.As(err, &mfaErr) {
		status = http.StatusUnauthorized
	}
	h.auditProtectedRefusal(r, email, app, keys, rbac.ActionSetProtected, err.Error(), status)
	return err
}

// checkProtectedChangeCaller returns nil when the caller may change the protected keys named: a user holding
// convox:env:set_protected who verified MFA on this request. API tokens never may: they can't do MFA.
func (h *Handler) checkProtectedChangeCaller(r *http.Request, named string) error {
	authUser, _ := auth.GetAuthUser(r.Context())
	if authUser != nil && authUser.IsAPIToken {
		return &envChangeDeniedError{message: "API tokens can't change protected env vars: " + named}
	}
	if authUser == nil || !h.callerCan(r, rbac.ResourceEnv, rbac.ActionSetProtected) {
		return &envChangeDeniedError{message: "You don't have permission to change protected env vars: " + named}
	}
	return h.verifyProtectedChangeMFA(r, authUser, named)
}

// verifyProtectedChangeMFA requires MFA verified on this request, even when the session's step-up window is
// still open. verifyMFAIfRequired skips the inline proof while that window is open, so the proof is verified
// here instead, at most once per request: TOTP codes and WebAuthn challenges are single-use. Verifying it
// refreshes the step-up window, which covers the rest of the command (e.g. the promote after --promote).
func (h *Handler) verifyProtectedChangeMFA(r *http.Request, authUser *auth.User, named string) error {
	if inlineMFAVerified(r) {
		return nil
	}
	if authUser.MFAType == "" || authUser.MFAValue == "" {
		return &protectedEnvMFAError{
			message: "Multi-factor authentication is required to change protected env vars: " + named,
		}
	}
	if h.mfaService == nil || h.database == nil {
		return &protectedEnvMFAError{message: "MFA verification is unavailable, so protected env vars can't change"}
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err == nil {
		err = h.verifyMFAByType(r, authUser, userRecord)
	}
	if err != nil {
		gtwlog.Warnf("proxy: MFA verification for a protected env change failed: %v", err)
		return &protectedEnvMFAError{message: "MFA verification failed, so protected env vars can't change: " + named}
	}
	h.updateSessionStepUp(authUser)
	markInlineMFAVerified(r)
	return nil
}

// auditProtectedRefusal records a refused protected env change, one entry per key. Values are never logged.
func (h *Handler) auditProtectedRefusal(
	r *http.Request,
	email, app string,
	keys []string,
	action rbac.Action,
	reason string,
	status int,
) {
	details, _ := json.Marshal(map[string]string{"error": reason})
	for _, key := range keys {
		h.logDeniedRBACAction(
			r, email, rbac.ResourceEnv, action, fmt.Sprintf("%s/%s", app, key), string(details), status,
		)
	}
}
