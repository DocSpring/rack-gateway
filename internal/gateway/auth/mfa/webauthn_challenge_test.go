package mfa

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
)

func newTestSession(t *testing.T, database *db.Database, userID int64) int64 {
	t.Helper()
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	session, err := database.CreateUserSession(
		userID, hex.EncodeToString(buf), time.Now().Add(time.Hour), "web", "", "", "127.0.0.1", "test", nil, nil,
	)
	require.NoError(t, err)
	return session.ID
}

type webAuthnFixture struct {
	service    *Service
	database   *db.Database
	user       *db.User
	method     *db.MFAMethod
	credential *webauthntest.MockCredential
}

func newWebAuthnFixture(t *testing.T) *webAuthnFixture {
	t.Helper()
	database := dbtest.NewDatabase(t)
	service, err := NewService(
		database, "Test Gateway", 24*time.Hour, 10*time.Minute, []byte("pepper"),
		"", "", "localhost", "http://localhost", nil,
	)
	require.NoError(t, err)
	user, err := database.CreateUser("key@example.com", "Key User", []string{"admin"})
	require.NoError(t, err)
	credential, err := webauthntest.GenerateMockCredential()
	require.NoError(t, err)
	method, err := database.CreateMFAMethod(
		user.ID, "webauthn", "Key", "", credential.ID, credential.PublicKey, nil, nil,
	)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))
	return &webAuthnFixture{service: service, database: database, user: user, method: method, credential: credential}
}

// assert starts a ceremony for startSession and verifies it for verifySession.
func (f *webAuthnFixture) assert(t *testing.T, startSession, verifySession *int64) (string, string, error) {
	t.Helper()
	options, challengeID, err := f.service.StartWebAuthnAssertion(f.user, startSession)
	require.NoError(t, err)
	assertionJSON, err := f.credential.GenerateAssertion(options, "http://localhost")
	require.NoError(t, err)
	_, err = f.service.VerifyWebAuthnAssertion(
		f.user, []byte(challengeID), []byte(assertionJSON), "127.0.0.1", "test", verifySession,
	)
	return challengeID, assertionJSON, err
}

func TestWebAuthnAssertionChallengeIsSingleUse(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	f.credential.Counter = 1

	challengeID, assertionJSON, err := f.assert(t, nil, nil)
	require.NoError(t, err)

	// Replaying the same assertion and challenge must fail.
	_, err = f.service.VerifyWebAuthnAssertion(
		f.user, []byte(challengeID), []byte(assertionJSON), "127.0.0.1", "test", nil,
	)
	require.ErrorIs(t, err, ErrWebAuthnChallenge)
}

func TestWebAuthnAssertionRejectsForgedOrForeignChallenge(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)

	// A made-up challenge ID is rejected.
	_, err := f.service.VerifyWebAuthnAssertion(
		f.user, []byte("wac_forged"), []byte(`{}`), "127.0.0.1", "test", nil,
	)
	require.ErrorIs(t, err, ErrWebAuthnChallenge)

	// A challenge started by one session can't be used by another session.
	sessionA := newTestSession(t, f.database, f.user.ID)
	sessionB := newTestSession(t, f.database, f.user.ID)
	_, _, err = f.assert(t, &sessionA, &sessionB)
	require.ErrorIs(t, err, ErrWebAuthnChallenge)

	// The owning session succeeds.
	f.credential.Counter = 1
	_, _, err = f.assert(t, &sessionA, &sessionA)
	require.NoError(t, err)

	// A challenge belonging to another user is rejected.
	other, err := f.database.CreateUser("other@example.com", "Other", []string{"admin"})
	require.NoError(t, err)
	_, challengeID, err := f.service.StartWebAuthnAssertion(f.user, nil)
	require.NoError(t, err)
	_, err = f.service.VerifyWebAuthnAssertion(other, []byte(challengeID), []byte(`{}`), "127.0.0.1", "test", nil)
	require.ErrorIs(t, err, ErrWebAuthnChallenge)
}

func TestWebAuthnAssertionChallengeExpires(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	options, challengeID, err := f.service.StartWebAuthnAssertion(f.user, nil)
	require.NoError(t, err)
	_, err = f.database.DB().Exec(
		"UPDATE webauthn_challenges SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1", challengeID,
	)
	require.NoError(t, err)

	assertionJSON, err := f.credential.GenerateAssertion(options, "http://localhost")
	require.NoError(t, err)
	_, err = f.service.VerifyWebAuthnAssertion(
		f.user, []byte(challengeID), []byte(assertionJSON), "127.0.0.1", "test", nil,
	)
	require.ErrorIs(t, err, ErrWebAuthnChallenge)
}

func TestWebAuthnAssertionRequiresUserVerification(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	f.credential.WithoutUserVerification = true

	_, _, err := f.assert(t, nil, nil)
	require.Error(t, err, "assertion without user verification (PIN) must be rejected")
	require.False(t, errors.Is(err, ErrWebAuthnChallenge))
}

func TestWebAuthnAssertionEnforcesSignatureCounter(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)

	f.credential.Counter = 10
	_, _, err := f.assert(t, nil, nil)
	require.NoError(t, err)
	stored, err := f.database.GetMFAMethodByID(f.method.ID)
	require.NoError(t, err)
	require.Equal(t, uint32(10), stored.SignCount, "new counter must be stored")

	// A counter that doesn't increase means the key may have been cloned.
	f.credential.Counter = 10
	_, _, err = f.assert(t, nil, nil)
	require.ErrorContains(t, err, "signature counter")
	f.credential.Counter = 7
	_, _, err = f.assert(t, nil, nil)
	require.ErrorContains(t, err, "signature counter")

	f.credential.Counter = 11
	_, _, err = f.assert(t, nil, nil)
	require.NoError(t, err)
}

func TestWebAuthnAssertionAllowsZeroCounterAuthenticators(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	// Many platform authenticators and passkey managers always report 0.
	for i := 0; i < 2; i++ {
		_, _, err := f.assert(t, nil, nil)
		require.NoError(t, err)
	}
}

// Two assertions with the same counter (a cloned key racing the real one): only one may succeed.
func TestWebAuthnSignCounterAdvancesAtomically(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	f.credential.Counter = 10
	_, _, err := f.assert(t, nil, nil)
	require.NoError(t, err)

	f.credential.Counter = 11
	first, firstID := f.signedAssertion(t)
	second, secondID := f.signedAssertion(t)
	results := make(chan error, 2)
	for _, pair := range [][2]string{{firstID, first}, {secondID, second}} {
		go func(challengeID, assertionJSON string) {
			_, err := f.service.VerifyWebAuthnAssertion(
				f.user, []byte(challengeID), []byte(assertionJSON), "127.0.0.1", "test", nil,
			)
			results <- err
		}(pair[0], pair[1])
	}
	succeeded := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "exactly one assertion with counter 11 may succeed")
}

func TestAdvanceMFAMethodSignCount(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	advance := func(count uint32) bool {
		ok, err := f.database.AdvanceMFAMethodSignCount(f.method.ID, count)
		require.NoError(t, err)
		return ok
	}
	require.True(t, advance(0), "authenticators that don't count stay at 0")
	require.True(t, advance(0))
	require.True(t, advance(5))
	require.False(t, advance(5), "the same counter twice")
	require.False(t, advance(0), "dropping back to 0")
	require.True(t, advance(6))
}

// signedAssertion starts a ceremony and signs it with the credential's current counter.
func (f *webAuthnFixture) signedAssertion(t *testing.T) (string, string) {
	t.Helper()
	options, challengeID, err := f.service.StartWebAuthnAssertion(f.user, nil)
	require.NoError(t, err)
	assertionJSON, err := f.credential.GenerateAssertion(options, "http://localhost")
	require.NoError(t, err)
	return assertionJSON, challengeID
}
