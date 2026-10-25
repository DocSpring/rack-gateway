package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// deleteApp deletes the app (convox:app:delete, an MFAAlways permission) as the admin, from a CLI session that
// stepped up a moment ago, with mfaCode as the inline TOTP code ("" sends none).
func (f *protectedEnvFixture) deleteApp(t *testing.T, mfaCode string) *httptest.ResponseRecorder {
	t.Helper()
	envReq, _ := f.request(t, f.admin, "", "", mfaCode, stepUpAgeFresh)
	authUser, ok := auth.GetAuthUser(envReq.Context())
	require.True(t, ok)
	req := httptest.NewRequest(http.MethodDelete, "/apps/app", nil)
	return f.proxy(req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, authUser)))
}

// An MFAAlways permission needs an MFA proof verified on the request itself: a fresh step-up doesn't cover it, and
// a proof can't be used twice.
func TestMFAAlwaysPermissionNeedsAProofOnTheRequest(t *testing.T) {
	f := newProtectedEnvFixture(t)
	require.NoError(t, settings.NewService(f.database).SetGlobalSettings(
		map[string]interface{}{settings.KeyAllowDestructiveActions: true}, nil))

	rr := f.deleteApp(t, "")
	require.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
	require.Equal(t, "always", rr.Header().Get("X-MFA-Level"))
	require.Contains(t, rr.Body.String(), "Multi-factor authentication is required for this action")

	code := f.totpCode(t)
	rr = f.deleteApp(t, code)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	rr = f.deleteApp(t, code)
	require.Equal(t, http.StatusUnauthorized, rr.Code, "a used code is refused: %s", rr.Body.String())
	require.Contains(t, rr.Body.String(), "MFA verification failed")
}

// processRequest is method on path with headers, as the admin from a CLI session that stepped up a moment ago, with
// mfaCode as the inline TOTP code ("" sends none).
func (f *protectedEnvFixture) processRequest(
	t *testing.T, method, path, mfaCode string, headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	envReq, _ := f.request(t, f.admin, "", "", mfaCode, stepUpAgeFresh)
	authUser, ok := auth.GetAuthUser(envReq.Context())
	require.True(t, ok)
	req := httptest.NewRequest(method, path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return f.proxy(req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, authUser)))
}

const processStartPath = "/apps/app/services/web/processes"

// requireFreshMFARefusal checks rr is the refusal of a request that needed an MFA proof of its own.
func requireFreshMFARefusal(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
	require.Equal(t, "always", rr.Header().Get("X-MFA-Level"))
	require.Contains(t, rr.Body.String(), "Multi-factor authentication is required for this action")
}

// Starting a process that runs a command needs an MFA proof on the request, even right after a step-up.
func TestProcessStartNeedsAProofOnTheRequest(t *testing.T) {
	f := newProtectedEnvFixture(t)
	command := map[string]string{"Command": "rails runner 'User.delete_all'"}

	requireFreshMFARefusal(t, f.processRequest(t, http.MethodPost, processStartPath, "", command))

	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), command)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}

// `convox run`'s placeholder start runs nothing, so it needs no proof: the command's proof goes with the exec. One
// for another of the app's releases (`run --release`) doesn't either. A placeholder start with an option that can
// run something else (a custom image's entrypoint, the service's lifecycle hooks) or a second command needs one.
func TestAttachedRunPlaceholderStartNeedsNoProof(t *testing.T) {
	f := newProtectedEnvFixture(t)
	placeholder := map[string]string{"Command": convox.AttachedRunPlaceholder}
	rr := f.processRequest(t, http.MethodPost, processStartPath, "", placeholder)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	placeholder["Release"] = "RNEW123"
	rr = f.processRequest(t, http.MethodPost, processStartPath, "", placeholder)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	for _, option := range []string{"Image", "Use-Service-Lifecycle", "Privileged"} {
		headers := map[string]string{"Command": convox.AttachedRunPlaceholder, option: "true"}
		requireFreshMFARefusal(t, f.processRequest(t, http.MethodPost, processStartPath, "", headers))
	}

	envReq, _ := f.request(t, f.admin, "", "", "", stepUpAgeFresh)
	req := httptest.NewRequest(http.MethodPost, processStartPath, nil)
	req.Header.Add("Command", convox.AttachedRunPlaceholder)
	req.Header.Add("Command", "rails runner 'User.delete_all'")
	requireFreshMFARefusal(t, f.proxy(req.WithContext(envReq.Context())))
}

// An exec runs a command in a running process: it needs a proof on the request, even right after a step-up.
func TestExecNeedsAProofOnTheRequest(t *testing.T) {
	f := newProtectedEnvFixture(t)
	upgrade := map[string]string{
		"Connection": "Upgrade", "Upgrade": "websocket", "Command": "rails runner 'User.delete_all'",
	}
	requireFreshMFARefusal(t, f.processRequest(t, http.MethodGet, "/apps/app/processes/p-1/exec", "", upgrade))
}
