package routes

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

var newUserBody = map[string]interface{}{"email": "new@example.com", "name": "New User", "roles": []string{"viewer"}}

// TestEverythingFailsClosedThroughTheRouter checks AuditBeforeActing is wired into the real router, with no
// exceptions: with the audit log down, changes, secret reveals, plain reads, MFA verification, login and logout are
// all refused before their handlers run, and the health check reports the outage.
func TestEverythingFailsClosedThroughTheRouter(t *testing.T) {
	e := newAuthzEnv(t)
	admin := e.enrolledSession(t, "admin@example.com")
	e.auditLog.SetChainWriter(audittest.FailingChainWriter{})

	w := e.do(t, admin.withMFA(t), http.MethodPost, "/api/v1/users", newUserBody)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "audit log unavailable; refusing to perform user.create")
	created, err := e.database.GetUser("new@example.com")
	require.NoError(t, err)
	require.Nil(t, created, "the refused request must not have created the user")

	refused := map[string]struct {
		method, path string
		body         interface{}
	}{
		"secret reveal":    {http.MethodGet, "/api/v1/apps/web/env?secrets=true", nil},
		"plain read":       {http.MethodGet, "/api/v1/users", nil},
		"MFA verification": {http.MethodPost, "/api/v1/auth/mfa/verify", map[string]string{"code": "000000"}},
	}
	for name, tc := range refused {
		w = e.do(t, admin.creds, tc.method, tc.path, tc.body)
		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s: %s", name, w.Body.String())
		require.Contains(t, w.Body.String(), "audit log unavailable; refusing to perform", name)
	}

	w = e.do(t, nil, http.MethodGet, "/api/v1/auth/web/login", nil)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.Contains(t, w.Header().Get("Location"), "error=audit_unavailable", "login must not start")

	w = e.do(t, admin.creds, http.MethodGet, "/api/v1/auth/web/logout", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	w = e.do(t, admin.creds, http.MethodGet, "/api/v1/users", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"the refused logout must not have revoked the session (a revoked one gets 401): %s", w.Body.String())

	w = e.do(t, nil, http.MethodGet, "/api/v1/health", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "audit log unavailable")
}

// TestGatewayChangeRecordsAttemptAndResult: through the real router, a change is recorded as attempted before
// its handler runs, and the handler's own entry is its result.
func TestGatewayChangeRecordsAttemptAndResult(t *testing.T) {
	e := newAuthzEnv(t)
	admin := e.enrolledSession(t, "admin@example.com")

	w := e.do(t, admin.withMFA(t), http.MethodPost, "/api/v1/users", newUserBody)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	rows, err := e.database.DB().Query(`
		SELECT status, COALESCE(request_id, '') FROM audit.audit_event
		WHERE action = 'user.create' AND user_email = 'admin@example.com'
		ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var statuses, requestIDs []string
	for rows.Next() {
		var status, requestID string
		require.NoError(t, rows.Scan(&status, &requestID))
		statuses = append(statuses, status)
		requestIDs = append(requestIDs, requestID)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{audit.StatusAttempted, audit.StatusSuccess}, statuses)
	require.NotEmpty(t, requestIDs[0])
	require.Equal(t, requestIDs[0], requestIDs[1])
}
