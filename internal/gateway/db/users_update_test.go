package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func newSession(t *testing.T, database *gwdb.Database, user *gwdb.User, fill string) (*gwdb.UserSession, error) {
	t.Helper()
	return database.CreateUserSession(
		user, strings.Repeat(fill, 64), time.Now().Add(time.Hour), "web", "", "", "127.0.0.1", "test", nil, nil,
	)
}

// A new email can hand the account to someone else: nothing issued to the old address keeps working, and the record
// of who created the user follows it.
func TestUpdateUserEmailRevokesSessionsAndTokens(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("old@example.com", "User", []string{"deployer"})
	require.NoError(t, err)
	require.NoError(t, database.SetResourceCreator(admin.ID, "user", "old@example.com"))
	_, err = newSession(t, database, user, "a")
	require.NoError(t, err)
	_, err = database.CreateAPIToken(strings.Repeat("b", 64), "ci", user.ID, []string{"convox:app:list"}, nil, nil)
	require.NoError(t, err)

	email := "new@example.com"
	result, err := database.UpdateUser(user.ID, gwdb.UserUpdate{Email: &email}, &admin.ID)
	require.NoError(t, err)
	require.True(t, result.EmailChanged)
	require.Equal(t, int64(1), result.RevokedSessions)
	require.Equal(t, int64(1), result.DeletedTokens)
	require.Equal(t, email, result.User.Email)
	require.Equal(t, []string{"deployer"}, result.User.Roles)

	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)
	tokens, err := database.ListAPITokensByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, tokens)
	creators, err := database.GetResourceCreators("user", []string{"old@example.com", email})
	require.NoError(t, err)
	require.Len(t, creators, 1)
	require.Equal(t, admin.ID, creators[email].UserID)
}

// Only the fields an update names are written: a name change never writes back roles changed meanwhile.
func TestUpdateUserWritesOnlyTheGivenFields(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("user@example.com", "User", []string{"admin"})
	require.NoError(t, err)
	_, err = newSession(t, database, user, "c")
	require.NoError(t, err)

	_, err = database.UpdateUser(user.ID, gwdb.UserUpdate{Roles: []string{"viewer"}}, nil)
	require.NoError(t, err)
	name := "Renamed"
	result, err := database.UpdateUser(user.ID, gwdb.UserUpdate{Name: &name}, nil)
	require.NoError(t, err)
	require.False(t, result.EmailChanged)
	require.Equal(t, "Renamed", result.User.Name)
	require.Equal(t, []string{"viewer"}, result.User.Roles)

	sameEmail := "USER@example.com"
	result, err = database.UpdateUser(user.ID, gwdb.UserUpdate{Email: &sameEmail}, nil)
	require.NoError(t, err)
	require.False(t, result.EmailChanged, "a change of case isn't a new email")
	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
}

func TestUpdateUserRefusesATakenEmailAndChangesNothing(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.CreateUser("taken@example.com", "Taken", []string{"viewer"})
	require.NoError(t, err)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)

	email, name := "taken@example.com", "Changed"
	_, err = database.UpdateUser(user.ID, gwdb.UserUpdate{Email: &email, Name: &name}, nil)
	require.ErrorIs(t, err, gwdb.ErrUserExists)
	unchanged, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", unchanged.Email)
	require.Equal(t, "User", unchanged.Name)

	_, err = database.UpdateUser(user.ID+1000, gwdb.UserUpdate{Name: &name}, nil)
	require.ErrorIs(t, err, gwdb.ErrUserNotFound)
	_, err = database.CreateUser("taken@example.com", "Again", []string{"admin"})
	require.ErrorIs(t, err, gwdb.ErrUserExists)
}

// A login that loaded the user before their email changed, or before they were locked, can't add a session after.
func TestCreateUserSessionRefusesAUserWhoChanged(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)
	loaded := *user

	email := "someone-else@example.com"
	_, err = database.UpdateUser(user.ID, gwdb.UserUpdate{Email: &email}, nil)
	require.NoError(t, err)
	_, err = newSession(t, database, &loaded, "d")
	require.ErrorIs(t, err, gwdb.ErrSessionUserChanged)

	renamed, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.NoError(t, database.LockUser(user.ID, "test", nil))
	_, err = newSession(t, database, renamed, "e")
	require.ErrorIs(t, err, gwdb.ErrSessionUserChanged)
}

// Locking a user revokes their sessions with the lock, so unlocking them later doesn't bring a stolen one back.
func TestLockingAUserRevokesTheirSessions(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)
	_, err = newSession(t, database, user, "f")
	require.NoError(t, err)

	require.NoError(t, database.LockUser(user.ID, "suspicious", &admin.ID))
	require.NoError(t, database.UnlockUser(user.ID, admin.ID))
	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)

	_, err = newSession(t, database, user, "0")
	require.NoError(t, err)
	entry := func(u gwdb.LockedUser) *gwdb.AuditLog {
		return &gwdb.AuditLog{
			UserEmail: u.Email, ActionType: "auth", Action: "user.lock", ResourceType: "user", Resource: u.Email,
			Status: "success", RBACDecision: "allow",
		}
	}
	locked, _, err := database.LockUserAudited(user.ID, "too many MFA failures", entry)
	require.NoError(t, err)
	require.NotNil(t, locked)
	sessions, err = database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func TestListUsersReportsMFAEnrollment(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)
	require.NoError(t, database.SetUserMFAEnrolled(user.ID, true))

	users, err := database.ListUsers()
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.True(t, users[0].MFAEnrolled)
}
