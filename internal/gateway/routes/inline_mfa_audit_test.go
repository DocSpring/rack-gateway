package routes_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

// An inline MFA proof changes the session (it completes a pending MFA challenge or opens the step-up window) only
// once the request's audit attempt is written. With the audit log down the request is refused, and the session is
// left as it was, on the gateway's own routes and on the CLI proxy.
func TestInlineMFAChangesNoSessionWhenTheAuditLogIsDown(t *testing.T) {
	cases := map[string]struct {
		verified bool
		method   string
		path     string
		headers  func(s *webSession) map[string]string
	}{
		"pending session on a gateway route": {
			method:  http.MethodGet,
			path:    "/api/v1/users",
			headers: func(s *webSession) map[string]string { return s.headers },
		},
		"step-up on a gateway route": {
			verified: true,
			method:   http.MethodPost,
			path:     "/api/v1/auth/mfa/enroll/totp/start",
			headers:  func(s *webSession) map[string]string { return s.headers },
		},
		"step-up on the CLI proxy": {
			verified: true,
			method:   http.MethodPost,
			path:     "/api/v1/rack-proxy/apps/web/restart",
			headers:  bearer,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newMFARouteEnv(t, "inline-audit@example.com")
			s := e.newSession(t, e.user)
			if tc.verified {
				e.markVerified(t, s, false)
			}
			before := e.loadSession(t, s)
			e.auditLog.SetChainWriter(audittest.FailingChainWriter{})

			w := e.do(tc.method, tc.path, "", withTOTP(tc.headers(s), e.currentCode(t)))
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

			after := e.loadSession(t, s)
			require.Equal(t, before.MFAVerifiedAt, after.MFAVerifiedAt, "the MFA challenge is unchanged")
			require.Equal(t, before.RecentStepUpAt, after.RecentStepUpAt, "no step-up window was opened")
		})
	}
}

// loadSession reads the session as stored.
func (e *mfaRouteEnv) loadSession(t *testing.T, s *webSession) *db.UserSession {
	t.Helper()
	session, err := e.database.GetSessionByID(s.session.ID)
	require.NoError(t, err)
	require.NotNil(t, session)
	return session
}
