package routes

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/audittest"
)

const cliLogoutPath = "/api/v1/auth/cli/logout"

// cliSession returns the token and bearer credentials of a new CLI session for userEmail.
func (e *authzEnv) cliSession(t *testing.T, userEmail string) (string, credentials) {
	t.Helper()
	user, err := e.database.GetUser(userEmail)
	require.NoError(t, err)
	sessionToken, _, err := e.sessions.CreateSession(user, auth.SessionMetadata{Channel: "cli"})
	require.NoError(t, err)
	return sessionToken, credentials{"Authorization": "Bearer " + sessionToken}
}

func (e *authzEnv) logoutStatuses(t *testing.T, email string) []string {
	t.Helper()
	rows, err := e.database.DB().Query(`
		SELECT status FROM audit.audit_event
		WHERE action = 'logout' AND resource = 'cli' AND user_email = $1
		ORDER BY id`, email)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var statuses []string
	for rows.Next() {
		var status string
		require.NoError(t, rows.Scan(&status))
		statuses = append(statuses, status)
	}
	require.NoError(t, rows.Err())
	return statuses
}

// TestCLILogoutRevokesTheSession: `rack-gateway logout` ends the session on the gateway, recorded as attempted
// before the revoke and successful after it, so a copied token stops working.
func TestCLILogoutRevokesTheSession(t *testing.T) {
	e := newAuthzEnv(t)
	sessionToken, creds := e.cliSession(t, "viewer@example.com")

	w := e.do(t, creds, http.MethodPost, cliLogoutPath, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"revoked":true}`, w.Body.String())

	_, err := e.sessions.ValidateSession(sessionToken, "", "")
	require.Error(t, err, "the session must be revoked")
	require.Equal(t, []string{"attempted", "success"}, e.logoutStatuses(t, "viewer@example.com"))

	w = e.do(t, creds, http.MethodPost, cliLogoutPath, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "not a valid session")
}

// TestCLILogoutIgnoresCookiesAndAPITokens: only a bearer session token is logged out. A browser's cookie is
// ignored (so another site can't log a browser out), and an API token isn't a session.
func TestCLILogoutIgnoresCookiesAndAPITokens(t *testing.T) {
	e := newAuthzEnv(t)
	web := e.webSession(t, "viewer@example.com")
	webToken := strings.TrimPrefix(web["Cookie"], "session_token=")

	w := e.do(t, web, http.MethodPost, cliLogoutPath, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	_, err := e.sessions.ValidateSession(webToken, "", "")
	require.NoError(t, err, "the web session must still be valid")

	token := e.apiToken(t, "admin@example.com", "gateway:user:list")
	w = e.do(t, token, http.MethodPost, cliLogoutPath, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// TestCLILogoutFailsClosed: with the audit log down, the logout is refused before it revokes anything.
func TestCLILogoutFailsClosed(t *testing.T) {
	e := newAuthzEnv(t)
	sessionToken, creds := e.cliSession(t, "viewer@example.com")
	e.auditLog.SetChainWriter(audittest.FailingChainWriter{})

	w := e.do(t, creds, http.MethodPost, cliLogoutPath, nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "audit log unavailable")

	_, err := e.sessions.ValidateSession(sessionToken, "", "")
	require.NoError(t, err, "the refused logout must not have revoked the session")
}
