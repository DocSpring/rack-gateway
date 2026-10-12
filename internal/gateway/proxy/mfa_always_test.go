package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

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
	require.NoError(t, settings.NewService(f.database).SetGlobalSetting(settings.KeyAllowDestructiveActions, true, nil))

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
