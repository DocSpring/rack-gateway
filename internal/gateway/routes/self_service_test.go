package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// enrolledUser is a user with a TOTP factor and a web session that has completed login MFA.
type enrolledUser struct {
	email       string
	creds       credentials
	sessionID   int64
	backupCodes []string
}

// enrolledSession enrolls the user in TOTP (first call only) and returns a web session that has
// completed MFA, as after a real login, so requests reach the handlers.
func (e *authzEnv) enrolledSession(t *testing.T, email string) *enrolledUser {
	t.Helper()
	user, err := e.database.GetUser(email)
	require.NoError(t, err)
	require.NotNil(t, user)

	var backupCodes []string
	if !user.MFAEnrolled {
		start, err := e.mfaService.StartTOTPEnrollment(user)
		require.NoError(t, err)
		// The previous TOTP step, so the enrollment code is never a replay of a later one.
		code, err := totp.GenerateCode(start.Secret, time.Now().Add(-30*time.Second))
		require.NoError(t, err)
		require.NoError(t, e.mfaService.ConfirmTOTP(user, start.MethodID, code))
		backupCodes = start.BackupCodes
		user, err = e.database.GetUser(email)
		require.NoError(t, err)
	}

	sessionToken, session, err := e.sessions.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	require.NoError(t, e.database.UpdateSessionMFAVerified(session.ID, time.Now(), nil))
	csrf, err := e.sessions.DeriveCSRFToken(sessionToken)
	require.NoError(t, err)
	return &enrolledUser{
		email:       email,
		creds:       credentials{"Cookie": "session_token=" + sessionToken, "X-CSRF-Token": csrf},
		sessionID:   session.ID,
		backupCodes: backupCodes,
	}
}

// withMFA returns the session credentials plus a single-use backup code, for MFAAlways routes.
func (u *enrolledUser) withMFA(t *testing.T) credentials {
	t.Helper()
	require.NotEmpty(t, u.backupCodes, "no backup codes left for %s", u.email)
	out := credentials{"X-MFA-TOTP": u.backupCodes[0]}
	u.backupCodes = u.backupCodes[1:]
	for k, v := range u.creds {
		out[k] = v
	}
	return out
}

// ownedToken creates an API token owned by ownerEmail directly through the token service.
func (e *authzEnv) ownedToken(t *testing.T, ownerEmail, name string, permissions ...string) *db.APIToken {
	t.Helper()
	owner, err := e.database.GetUser(ownerEmail)
	require.NoError(t, err)
	resp, err := e.tokens.GenerateAPIToken(&token.APITokenRequest{
		Name: name, UserID: owner.ID, Permissions: permissions,
	})
	require.NoError(t, err)
	return resp.APIToken
}

func decodeJSON(t *testing.T, body []byte, out interface{}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(body, out), string(body))
}

// TestRoleRouteMatrix checks representative routes for every role and for a CI token, asserting the
// exact status each caller gets.
func TestRoleRouteMatrix(t *testing.T) {
	e := newAuthzEnv(t)
	callers := map[string]credentials{
		"viewer":   e.enrolledSession(t, "viewer@example.com").creds,
		"ops":      e.enrolledSession(t, "ops@example.com").creds,
		"deployer": e.enrolledSession(t, "deployer@example.com").creds,
		"admin":    e.enrolledSession(t, "admin@example.com").creds,
		"cicd":     e.apiToken(t, "admin@example.com", rbacRolePermissions(t, "cicd")...),
	}
	ok, forbidden := http.StatusOK, http.StatusForbidden
	cases := []struct {
		method, path string
		want         map[string]int
	}{
		{"GET", "/api/v1/info", map[string]int{"viewer": ok, "ops": ok, "deployer": ok, "admin": ok, "cicd": ok}},
		{"GET", "/api/v1/users", map[string]int{
			"viewer": ok, "ops": ok, "deployer": ok, "admin": ok, "cicd": forbidden,
		}},
		{"GET", "/api/v1/api-tokens", map[string]int{
			"viewer": ok, "ops": ok, "deployer": ok, "admin": ok, "cicd": forbidden,
		}},
		{"GET", "/api/v1/users/admin@example.com/sessions", map[string]int{
			"viewer": forbidden, "ops": forbidden, "deployer": forbidden, "admin": ok, "cicd": forbidden,
		}},
		{"GET", "/api/v1/audit-logs", map[string]int{
			"viewer": forbidden, "ops": forbidden, "deployer": forbidden, "admin": ok, "cicd": forbidden,
		}},
		{"GET", "/api/v1/deploy-approval-requests", map[string]int{
			"viewer": forbidden, "ops": forbidden, "deployer": forbidden, "admin": ok, "cicd": forbidden,
		}},
	}
	for _, tc := range cases {
		for caller, want := range tc.want {
			w := e.do(t, callers[caller], tc.method, tc.path, nil)
			require.Equalf(t, want, w.Code, "%s: %s %s => %s", caller, tc.method, tc.path, w.Body.String())
		}
	}

	// Every human can reach their own profile, sessions and audit trail.
	for _, email := range []string{"viewer@example.com", "ops@example.com", "deployer@example.com"} {
		creds := e.enrolledSession(t, email).creds
		for _, path := range []string{"/api/v1/users/%s", "/api/v1/users/%s/sessions", "/api/v1/users/%s/audit-logs"} {
			w := e.do(t, creds, http.MethodGet, fmt.Sprintf(path, email), nil)
			require.Equalf(t, http.StatusOK, w.Code, "%s: GET %s => %s", email, path, w.Body.String())
		}
	}
}

func TestTeamDirectoryHidesAdminDetailsFromNonAdmins(t *testing.T) {
	e := newAuthzEnv(t)
	admin, err := e.database.GetUser("admin@example.com")
	require.NoError(t, err)
	ops, err := e.database.GetUser("ops@example.com")
	require.NoError(t, err)
	require.NoError(t, e.database.LockUser(ops.ID, "suspicious login", &admin.ID))

	find := func(users []db.User, email string) db.User {
		for _, u := range users {
			if u.Email == email {
				return u
			}
		}
		t.Fatalf("%s missing from directory", email)
		return db.User{}
	}

	var directory []db.User
	w := e.do(t, e.enrolledSession(t, "viewer@example.com").creds, http.MethodGet, "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decodeJSON(t, w.Body.Bytes(), &directory)
	require.Len(t, directory, 4)
	entry := find(directory, "ops@example.com")
	require.Equal(t, []string{"ops"}, entry.Roles)
	require.NotNil(t, entry.LockedAt, "teammates can see an account is locked")
	require.Empty(t, entry.LockedReason)
	require.Empty(t, entry.LockedByEmail)

	var full []db.User
	w = e.do(t, e.enrolledSession(t, "admin@example.com").creds, http.MethodGet, "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decodeJSON(t, w.Body.Bytes(), &full)
	require.Equal(t, "suspicious login", find(full, "ops@example.com").LockedReason)
}

func TestUsersManageOnlyTheirOwnSessions(t *testing.T) {
	e := newAuthzEnv(t)
	viewer := e.enrolledSession(t, "viewer@example.com")
	viewerLaptop := e.enrolledSession(t, "viewer@example.com")
	admin := e.enrolledSession(t, "admin@example.com")

	var sessions []map[string]interface{}
	w := e.do(t, viewer.creds, http.MethodGet, "/api/v1/users/viewer@example.com/sessions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	decodeJSON(t, w.Body.Bytes(), &sessions)
	require.Len(t, sessions, 2)

	// Another user's session, addressed through the caller's own account, is not found.
	path := fmt.Sprintf("/api/v1/users/viewer@example.com/sessions/%d/revoke", admin.sessionID)
	w = e.do(t, viewer.withMFA(t), http.MethodPost, path, nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// Addressed through the other user's account, it is forbidden.
	path = fmt.Sprintf("/api/v1/users/admin@example.com/sessions/%d/revoke", admin.sessionID)
	w = e.do(t, viewer.creds, http.MethodPost, path, nil)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	path = fmt.Sprintf("/api/v1/users/viewer@example.com/sessions/%d/revoke", viewerLaptop.sessionID)
	w = e.do(t, viewer.withMFA(t), http.MethodPost, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.do(t, viewerLaptop.creds, http.MethodGet, "/api/v1/info", nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, "revoked session must stop working")

	adminSessions, err := e.database.ListActiveSessionsByUser(mustUserID(t, e, "admin@example.com"))
	require.NoError(t, err)
	require.Len(t, adminSessions, 1, "the admin's session must survive")
}

func TestSelfRuleMatchesTheExactEmail(t *testing.T) {
	e := newAuthzEnv(t)
	viewer := e.enrolledSession(t, "viewer@example.com")
	w := e.do(t, viewer.creds, http.MethodGet, "/api/v1/users/VIEWER@example.com", nil)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	w = e.do(t, viewer.creds, http.MethodGet, "/api/v1/users/viewer@example.com", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUserAuditTrailIsScopedToTheUser(t *testing.T) {
	e := newAuthzEnv(t)
	for _, entry := range []*db.AuditLog{
		{UserEmail: "viewer@example.com", ActionType: "convox", Action: "app.list", Status: "success"},
		{UserEmail: "admin@example.com", ActionType: "users", Action: "user.create", Status: "success"},
	} {
		require.NoError(t, e.database.CreateAuditLog(entry))
	}
	viewer := e.enrolledSession(t, "viewer@example.com")

	// A user filter in the query string can't widen the results to someone else.
	w := e.do(t, viewer.creds, http.MethodGet,
		"/api/v1/users/viewer@example.com/audit-logs?range=24h&user=admin@example.com", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Logs []db.AuditLog `json:"logs"`
	}
	decodeJSON(t, w.Body.Bytes(), &resp)
	require.NotEmpty(t, resp.Logs)
	for _, log := range resp.Logs {
		require.Equal(t, "viewer@example.com", log.UserEmail)
	}
}

func mustUserID(t *testing.T, e *authzEnv, email string) int64 {
	t.Helper()
	user, err := e.database.GetUser(email)
	require.NoError(t, err)
	require.NotNil(t, user)
	return user.ID
}
