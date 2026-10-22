package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func startBoundCLILogin(t *testing.T, database *gwdb.Database, state, email string) {
	t.Helper()
	require.NoError(t, database.CreateCLILoginState(gwdb.NewCLILogin{State: state}))
	bound, err := database.BindCLILoginBrowser(state, "binding-"+state, email, "User")
	require.NoError(t, err)
	require.True(t, bound)
}

// A failed or canceled CLI login stays failed: an MFA result that arrives afterwards doesn't revive it, and an
// unbound login can't be verified at all.
func TestCLILoginMFAResultNeedsALiveBoundLogin(t *testing.T) {
	database := dbtest.NewDatabase(t)
	startBoundCLILogin(t, database, "canceled", "user@example.com")
	require.NoError(t, database.FailCLILoginState("canceled", "canceled"))

	require.ErrorIs(t, database.MarkCLILoginVerified("canceled", nil), gwdb.ErrCLILoginEnded)
	require.ErrorIs(t, database.MarkCLILoginExempt("canceled"), gwdb.ErrCLILoginEnded)
	record, err := database.GetCLILoginState("canceled")
	require.NoError(t, err)
	require.Equal(t, "canceled", record.LoginError.String)
	require.False(t, record.MFAVerifiedAt.Valid)
	ok, err := database.SetCLILoginCode("canceled", "code-hash")
	require.NoError(t, err)
	require.False(t, ok, "no login code for a canceled login")

	require.NoError(t, database.CreateCLILoginState(gwdb.NewCLILogin{State: "unbound"}))
	require.ErrorIs(t, database.MarkCLILoginVerified("unbound", nil), gwdb.ErrCLILoginEnded)
}

// An admin's MFA reset ends the user's CLI logins in progress, even one that already passed MFA, and a login that
// loaded the user before the reset can't create a session after it.
func TestResetUserMFAEndsCLILoginsInProgress(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)
	method, err := database.CreateMFAMethod(user.ID, "totp", "App", "JBSWY3DPEHPK3PXP", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.FinalizeMFAEnrollment(user.ID, method.ID, time.Now(), nil))
	loaded, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.True(t, loaded.MFAEnrolled)

	startBoundCLILogin(t, database, "verified", "User@Example.com")
	require.NoError(t, database.MarkCLILoginVerified("verified", &method.ID))
	startBoundCLILogin(t, database, "someone-else", "admin@example.com")

	require.NoError(t, database.ResetUserMFA(user.ID, admin.ID))

	record, err := database.GetCLILoginState("verified")
	require.NoError(t, err)
	require.Nil(t, record, "the verified login is gone")
	other, err := database.GetCLILoginState("someone-else")
	require.NoError(t, err)
	require.NotNil(t, other, "other users' logins are kept")

	_, err = database.CreateUserSession(loaded, strings.Repeat("e", 64), time.Now().Add(time.Hour), "cli", "", "", "",
		"", nil, nil)
	require.ErrorIs(t, err, gwdb.ErrSessionUserChanged)
}

// A login that checked the user just before an admin's MFA reset, and is still adding its session, holds a share
// lock on the user's row (CreateUserSession). The reset waits for it, then revokes the session it added.
func TestResetUserMFARevokesTheSessionOfALoginInFlight(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)
	method, err := database.CreateMFAMethod(user.ID, "totp", "App", "JBSWY3DPEHPK3PXP", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.FinalizeMFAEnrollment(user.ID, method.ID, time.Now(), nil))

	login, err := database.DB().Begin()
	require.NoError(t, err)
	defer func() { _ = login.Rollback() }()
	var checked int64
	require.NoError(t, login.QueryRow(`SELECT id FROM users WHERE id = $1 FOR SHARE`, user.ID).Scan(&checked))

	reset := make(chan error, 1)
	go func() { reset <- database.ResetUserMFA(user.ID, admin.ID) }()
	select {
	case err := <-reset:
		t.Fatalf("the reset finished while a login held the user's row: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	_, err = login.Exec(`INSERT INTO user_sessions (user_id, token_hash, expires_at, channel)
		VALUES ($1, $2, NOW() + interval '1 hour', 'web')`, user.ID, strings.Repeat("f", 64))
	require.NoError(t, err)
	require.NoError(t, login.Commit())
	require.NoError(t, <-reset)

	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions, "the reset revoked the session the login added")
}
