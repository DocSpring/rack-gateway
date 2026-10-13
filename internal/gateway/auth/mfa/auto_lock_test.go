package mfa

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type sentEmail struct {
	to      []string
	subject string
}

type recordingSender struct{ sent []sentEmail }

func (r *recordingSender) Send(to, subject, textBody, htmlBody string) error {
	return r.SendMany([]string{to}, subject, textBody, htmlBody)
}

func (r *recordingSender) SendMany(to []string, subject, _, _ string) error {
	r.sent = append(r.sent, sentEmail{to: to, subject: subject})
	return nil
}

// autoLockFixture is a viewer about to fail MFA, an admin, and a locked admin.
func autoLockFixture(t *testing.T) (*Service, *db.Database, *db.User, *recordingSender) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	sender := &recordingSender{}
	svc, err := NewService(database, "Test", nil, []byte("test-pepper"), "", "", "", "", sender)
	require.NoError(t, err)

	viewer, err := database.CreateUser("viewer@example.com", "Viewer", []string{"viewer"})
	require.NoError(t, err)
	_, err = database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	lockedAdmin, err := database.CreateUser("locked-admin@example.com", "Locked Admin", []string{"admin"})
	require.NoError(t, err)
	require.NoError(t, database.LockUser(lockedAdmin.ID, "away", nil))
	return svc, database, viewer, sender
}

func failTOTP(svc *Service, user *db.User, times int) error {
	var err error
	for i := 0; i < times; i++ {
		_, err = svc.VerifyTOTP(user, "000000", "1.2.3.4", "test-agent", nil)
	}
	return err
}

// The automatic lock is recorded as a user.lock by the system, and the user and every unlocked admin are emailed.
func TestAutoLockIsAuditedAndAdminsAreEmailed(t *testing.T) {
	t.Parallel()
	svc, database, viewer, sender := autoLockFixture(t)

	require.ErrorIs(t, failTOTP(svc, viewer, autoLockAttempts), errAccountLocked)
	locked, err := database.IsUserLocked(viewer.ID)
	require.NoError(t, err)
	require.True(t, locked)

	logs, err := database.GetAuditLogs(audit.SystemActor, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "user.lock", logs[0].Action)
	require.Equal(t, "viewer@example.com", logs[0].Resource)
	require.Contains(t, logs[0].Details, autoLockReason)

	require.Equal(t, []sentEmail{
		{to: []string{"viewer@example.com"}, subject: "Account Locked - Multiple Failed Login Attempts"},
		{to: []string{"admin@example.com"}, subject: "User locked: viewer@example.com"},
	}, sender.sent)

	// A locked account is refused before any attempt is checked, so it isn't locked (or announced) again.
	require.Error(t, failTOTP(svc, viewer, 1))
	logs, err = database.GetAuditLogs(audit.SystemActor, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Len(t, sender.sent, 2)
}

// A user is never locked without the lock's audit event: when it can't be appended, nothing is locked or sent.
func TestAutoLockFailsClosed(t *testing.T) {
	t.Parallel()
	svc, database, viewer, sender := autoLockFixture(t)
	_, err := database.DB().Exec(`ALTER FUNCTION audit.append_audit_event RENAME TO append_audit_event_unavailable`)
	require.NoError(t, err)

	err = failTOTP(svc, viewer, autoLockAttempts)
	require.ErrorIs(t, err, db.ErrAuditAppend)
	locked, lockErr := database.IsUserLocked(viewer.ID)
	require.NoError(t, lockErr)
	require.False(t, locked)
	require.Empty(t, sender.sent)
}
