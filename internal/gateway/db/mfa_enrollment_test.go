package db_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func newEnrollmentUser(t *testing.T) (*gwdb.Database, *gwdb.User) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("enroll@example.com", "Enroll", []string{"viewer"})
	require.NoError(t, err)
	return database, user
}

func newEnrollmentSession(t *testing.T, database *gwdb.Database, user *gwdb.User, n int) *gwdb.UserSession {
	t.Helper()
	session, err := database.CreateUserSession(user, fmt.Sprintf("%064d", n), time.Now().Add(time.Hour),
		"web", "", "", "", "", nil, nil)
	require.NoError(t, err)
	return session
}

// A first enrollment confirms the method, marks the user enrolled and takes away the MFA exemption their sessions
// had, all at once.
func TestFinalizeMFAEnrollmentClearsExemptSessions(t *testing.T) {
	database, user := newEnrollmentUser(t)
	session := newEnrollmentSession(t, database, user, 1)
	require.NoError(t, database.ExemptSessionFromMFAChallenge(session.ID, time.Now(), nil))
	method, err := database.CreateMFAMethod(user.ID, "totp", "App", "JBSWY3DPEHPK3PXP", nil, nil, nil, nil)
	require.NoError(t, err)

	require.NoError(t, database.FinalizeMFAEnrollment(user.ID, method.ID, time.Now(), nil))

	reloaded, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.True(t, reloaded.MFAEnrolled)
	require.NotNil(t, reloaded.MFAEnforcedAt)
	confirmed, err := database.GetMFAMethodByID(method.ID)
	require.NoError(t, err)
	require.NotNil(t, confirmed.ConfirmedAt)
	stored, err := database.GetSessionByID(session.ID)
	require.NoError(t, err)
	require.Nil(t, stored.MFAVerifiedAt, "the session was exempt only because the user had no factor")

	// A login that decided before the enrollment that no challenge was needed can't exempt its session now.
	late := newEnrollmentSession(t, database, user, 2)
	require.ErrorIs(t, database.ExemptSessionFromMFAChallenge(late.ID, time.Now(), nil),
		gwdb.ErrMFAEnrolledDuringSignIn)
	stored, err = database.GetSessionByID(late.ID)
	require.NoError(t, err)
	require.Nil(t, stored.MFAVerifiedAt)
}

// A new enrollment deletes the user's unconfirmed methods: confirming one of them afterwards enrolls nothing.
func TestFinalizeMFAEnrollmentNeedsThePendingMethod(t *testing.T) {
	database, user := newEnrollmentUser(t)
	pending, err := database.CreateMFAMethod(user.ID, "webauthn_pending", "Security Key", "", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.DeleteUnconfirmedMFAMethods(user.ID))

	credential := &gwdb.WebAuthnCredential{Label: "Key", ID: []byte("cred"), PublicKey: []byte("pk"), SignCount: 3}
	err = database.FinalizeMFAEnrollment(user.ID, pending.ID, time.Now(), credential)
	require.ErrorIs(t, err, gwdb.ErrMFAMethodNotFound)
	reloaded, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.False(t, reloaded.MFAEnrolled)

	require.ErrorIs(t, database.ConfirmMFAMethod(pending.ID, time.Now()), gwdb.ErrMFAMethodNotFound)
}

// A security key's credential is stored on its pending method in the same transaction that confirms it.
func TestFinalizeMFAEnrollmentStoresTheCredential(t *testing.T) {
	database, user := newEnrollmentUser(t)
	pending, err := database.CreateMFAMethod(user.ID, "webauthn_pending", "Security Key", "", nil, nil, nil, nil)
	require.NoError(t, err)
	other, err := database.CreateUser("other@example.com", "Other", []string{"viewer"})
	require.NoError(t, err)

	credential := &gwdb.WebAuthnCredential{
		Label: "YubiKey", ID: []byte("cred"), PublicKey: []byte("pk"), Transports: []string{"usb"},
		Metadata: []byte(`{}`), SignCount: 7,
	}
	require.ErrorIs(t, database.FinalizeMFAEnrollment(other.ID, pending.ID, time.Now(), credential),
		gwdb.ErrMFAMethodNotFound, "another user's method")
	require.NoError(t, database.FinalizeMFAEnrollment(user.ID, pending.ID, time.Now(), credential))

	method, err := database.GetMFAMethodByID(pending.ID)
	require.NoError(t, err)
	require.Equal(t, "webauthn", method.Type)
	require.Equal(t, "YubiKey", method.Label)
	require.Equal(t, []byte("cred"), method.CredentialID)
	require.Equal(t, uint32(7), method.SignCount)
	require.NotNil(t, method.ConfirmedAt)
}

// Concurrent attempts are counted one at a time: no more than the limit get through together.
func TestReserveMFAAttemptKeepsConcurrentAttemptsWithinTheLimit(t *testing.T) {
	database, user := newEnrollmentUser(t)
	const tries, limit = 20, 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	reserved, refused := 0, 0
	for range tries {
		wg.Go(func() {
			req := gwdb.MFAAttemptRequest{UserID: user.ID, MethodType: gwdb.MFAMethodTypeTOTP}
			attempt, err := database.ReserveMFAAttempt(req, limit, 5)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				reserved++
				assert.NoError(t, attempt.Finish(nil, false, "invalid_code"))
				return
			}
			assert.ErrorIs(t, err, gwdb.ErrMFAAttemptLimit)
			refused++
		})
	}
	wg.Wait()
	require.Equal(t, limit, reserved)
	require.Equal(t, tries-limit, refused)

	require.NoError(t, database.LockUser(user.ID, "test", nil))
	locked := gwdb.MFAAttemptRequest{UserID: user.ID, MethodType: gwdb.MFAMethodTypeWebAuthn}
	_, err := database.ReserveMFAAttempt(locked, limit, 5)
	require.ErrorIs(t, err, gwdb.ErrUserLocked)
}

// Replacing backup codes holds the user's row lock: concurrent replacements leave exactly one set.
func TestConcurrentBackupCodeReplacementsLeaveOneSet(t *testing.T) {
	database, user := newEnrollmentUser(t)
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Go(func() {
			hashes := make([]string, 10)
			for j := range hashes {
				hashes[j] = fmt.Sprintf("set-%d-code-%d", i, j)
			}
			assert.NoError(t, database.ReplaceBackupCodes(user.ID, hashes))
		})
	}
	wg.Wait()

	codes, err := database.ListBackupCodes(user.ID)
	require.NoError(t, err)
	require.Len(t, codes, 10)
	for _, code := range codes {
		require.Equal(t, codes[0].CodeHash[:6], code.CodeHash[:6], "every code is from the same set")
	}
}
