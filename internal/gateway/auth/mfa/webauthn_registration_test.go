package mfa

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
)

type registrationAttempt struct {
	methodID     int64
	registration string
}

// startRegistration begins enrollment of a new key for the fixture user from session and signs the
// registration with key.
func (f *webAuthnFixture) startRegistration(
	t *testing.T,
	session int64,
	key *webauthntest.MockCredential,
) registrationAttempt {
	t.Helper()
	result, err := f.service.StartWebAuthnEnrollment(f.user, session)
	require.NoError(t, err)
	registration, err := key.GenerateRegistration(result.PublicKeyOptions, "http://localhost")
	require.NoError(t, err)
	return registrationAttempt{methodID: result.MethodID, registration: registration}
}

func (f *webAuthnFixture) confirmRegistration(t *testing.T, session int64, attempt registrationAttempt) error {
	t.Helper()
	_, err := f.service.ConfirmWebAuthnEnrollment(
		f.user, session, attempt.methodID, []byte(attempt.registration), "New key",
	)
	return err
}

func newRegistrationKey(t *testing.T) *webauthntest.MockCredential {
	t.Helper()
	key, err := webauthntest.GenerateMockCredential()
	require.NoError(t, err)
	return key
}

func TestWebAuthnRegistrationStoresCredentialAndCounter(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	session := newTestSession(t, f.database, f.user.ID)
	key := newRegistrationKey(t)
	key.Counter = 3

	attempt := f.startRegistration(t, session, key)
	require.NoError(t, f.confirmRegistration(t, session, attempt))

	stored, err := f.database.GetMFAMethodByID(attempt.methodID)
	require.NoError(t, err)
	require.Equal(t, "webauthn", stored.Type)
	require.Equal(t, key.ID, stored.CredentialID)
	require.Equal(t, uint32(3), stored.SignCount)
}

func TestWebAuthnRegistrationBoundToStartingSession(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	sessionA := newTestSession(t, f.database, f.user.ID)
	sessionB := newTestSession(t, f.database, f.user.ID)

	attempt := f.startRegistration(t, sessionA, newRegistrationKey(t))
	require.ErrorIs(t, f.confirmRegistration(t, sessionB, attempt), ErrWebAuthnChallenge)
}

func TestWebAuthnRegistrationChallengeExpires(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	session := newTestSession(t, f.database, f.user.ID)
	attempt := f.startRegistration(t, session, newRegistrationKey(t))

	_, err := f.database.DB().Exec(
		"UPDATE webauthn_challenges SET expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1", session,
	)
	require.NoError(t, err)
	require.ErrorIs(t, f.confirmRegistration(t, session, attempt), ErrWebAuthnChallenge)
}

func TestWebAuthnRegistrationCannotBeReplayed(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	session := newTestSession(t, f.database, f.user.ID)
	attempt := f.startRegistration(t, session, newRegistrationKey(t))

	require.NoError(t, f.confirmRegistration(t, session, attempt))
	require.Error(t, f.confirmRegistration(t, session, attempt), "a registration can't be confirmed twice")
}

func TestWebAuthnRegistrationRequiresUserVerification(t *testing.T) {
	t.Parallel()
	f := newWebAuthnFixture(t)
	session := newTestSession(t, f.database, f.user.ID)
	key := newRegistrationKey(t)
	key.WithoutUserVerification = true

	attempt := f.startRegistration(t, session, key)
	err := f.confirmRegistration(t, session, attempt)
	require.Error(t, err, "a key registered without its PIN or biometric must be rejected")
	require.NotErrorIs(t, err, ErrWebAuthnChallenge)
}
