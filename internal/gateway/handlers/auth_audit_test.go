package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

func (e *loopbackEnv) cliSessionCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, e.database.DB().QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE channel = 'cli'`).
		Scan(&count))
	return count
}

// A CLI login the audit log can't record isn't given its URL, so it can't go on.
func TestCLILoginStartIsRefusedWhenTheAuditLogIsDown(t *testing.T) {
	e := newLoopbackEnv(t, false)
	e.auditLog.SetChainWriter(audittest.FailingChainWriter{})

	res := e.send(t, http.MethodPost, "/api/v1/auth/cli/start", validStartRequest())
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Empty(t, body["auth_url"])
	require.Contains(t, body["error"], "audit log unavailable")
}

// The CLI session isn't created when its attempt can't be recorded.
func TestCLILoginCompleteIsRefusedWhenTheAuditLogIsDown(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)
	e.auditLog.SetChainWriter(audittest.FailingChainWriter{})

	res := e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.Zero(t, e.cliSessionCount(t), "no session without its audit record")
}

// A CLI session's creation is recorded as attempted before the session exists, and its result after, under the
// same request ID.
func TestCLILoginCompleteRecordsTheSessionCreation(t *testing.T) {
	e := newLoopbackEnv(t, false)
	state, binding := e.startAndBind(t)
	code := e.loginCodeFromBrowser(t, state, binding)

	res := e.complete(t, code, loopbackTestVerifier)
	require.Equal(t, http.StatusOK, res.StatusCode)

	rows, err := e.database.DB().Query(`
		SELECT status, COALESCE(request_id, '') FROM audit.audit_event
		WHERE action = $1 AND user_email = 'user@example.com' ORDER BY id`, audit.ActionSessionCreate)
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
