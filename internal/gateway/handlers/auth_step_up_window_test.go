package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Only a verified factor opens a session's step-up window. A login that needs no MFA challenge, or that a trusted
// device lets skip it, verifies none, and neither does trusting a device.

type stepUpTestEnv struct {
	handler  *AuthHandler
	database *db.Database
	sessions *auth.SessionManager
	mfa      *mfa.Service
}

func newStepUpTestEnv(t *testing.T) *stepUpTestEnv {
	t.Helper()
	database := dbtest.NewDatabase(t)
	sessions := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	mfaService, err := mfa.NewService(database, "Rack Gateway", nil, []byte("mfa-pepper-for-tests"),
		"", "", "", "", nil)
	require.NoError(t, err)
	handler := NewAuthHandler(&fakeOAuth{}, database, &config.Config{DevMode: true}, sessions, mfaService,
		settings.NewService(database), nil, audit.NewLogger(database))
	return &stepUpTestEnv{handler: handler, database: database, sessions: sessions, mfa: mfaService}
}

func (env *stepUpTestEnv) newSession(t *testing.T, email string, enrolled bool) (*db.User, *db.UserSession) {
	t.Helper()
	user, err := env.database.CreateUser(email, "User", []string{"viewer"})
	require.NoError(t, err)
	require.NoError(t, env.database.SetUserMFAEnrolled(user.ID, enrolled))
	user, err = env.database.GetUser(email)
	require.NoError(t, err)
	_, session, err := env.sessions.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	return user, session
}

func (env *stepUpTestEnv) storedSession(t *testing.T, id int64) *db.UserSession {
	t.Helper()
	session, err := env.database.GetSessionByID(id)
	require.NoError(t, err)
	require.NotNil(t, session)
	return session
}

func TestTrustedDeviceLoginOpensNoStepUpWindow(t *testing.T) {
	env := newStepUpTestEnv(t)
	user, session := env.newSession(t, "trusted@example.com", true)
	device, err := env.mfa.MintTrustedDevice(user.ID, "192.0.2.1", "Test Browser")
	require.NoError(t, err)

	c, _ := newTestContext(http.MethodGet, "/api/v1/auth/web/callback")
	c.Request.AddCookie(testCookie(trustedDeviceCookie, device.Token))
	c.Request.Header.Set("User-Agent", "Test Browser")
	require.NoError(t, env.handler.handlePostLoginMFA(c, user, session))

	stored := env.storedSession(t, session.ID)
	require.NotNil(t, stored.MFAVerifiedAt, "the trusted device skips the MFA challenge")
	require.NotNil(t, stored.TrustedDeviceID)
	require.Equal(t, device.RecordID, *stored.TrustedDeviceID)
	require.Nil(t, stored.RecentStepUpAt, "a trusted device is not a factor")
	require.Nil(t, session.RecentStepUpAt)
}

func TestLoginWithoutAnMFAChallengeOpensNoStepUpWindow(t *testing.T) {
	env := newStepUpTestEnv(t)
	user, session := env.newSession(t, "not-enrolled@example.com", false)

	c, _ := newTestContext(http.MethodGet, "/api/v1/auth/web/callback")
	require.NoError(t, env.handler.handlePostLoginMFA(c, user, session))

	stored := env.storedSession(t, session.ID)
	require.NotNil(t, stored.MFAVerifiedAt)
	require.Nil(t, stored.RecentStepUpAt)
}

func TestTrustCurrentDeviceKeepsTheStepUpWindow(t *testing.T) {
	env := newStepUpTestEnv(t)
	user, session := env.newSession(t, "trust@example.com", true)
	steppedUpAt := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Microsecond)
	require.NoError(t, env.sessions.UpdateSessionMFAVerified(session.ID, steppedUpAt, nil))
	session.MFAVerifiedAt = &steppedUpAt
	session.RecentStepUpAt = &steppedUpAt

	c, w := newTestContext(http.MethodPost, "/api/v1/auth/mfa/trusted-devices/trust")
	c.Request.Header.Set("User-Agent", "Test Browser")
	authUser := &auth.User{Email: user.Email, Name: user.Name, Roles: user.Roles, Session: session}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
	env.handler.TrustCurrentDevice(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp VerifyMFAResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.TrustedDeviceCookie)
	require.WithinDuration(t, steppedUpAt.Add(env.handler.stepUpWindow()), resp.RecentStepUpExpiresAt, time.Second)

	stored := env.storedSession(t, session.ID)
	require.NotNil(t, stored.TrustedDeviceID)
	require.NotNil(t, stored.RecentStepUpAt)
	require.WithinDuration(t, steppedUpAt, *stored.RecentStepUpAt, time.Millisecond, "the window must not restart")
	require.WithinDuration(t, steppedUpAt, *stored.MFAVerifiedAt, time.Millisecond)
}

func TestCLISessionStepUpWindowNeedsAVerifiedFactor(t *testing.T) {
	for _, tt := range []struct {
		name   string
		exempt bool
	}{
		{name: "verified factor", exempt: false},
		{name: "no MFA enforced", exempt: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newStepUpTestEnv(t)
			_, session := env.newSession(t, "cli@example.com", !tt.exempt)
			verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
			record := &db.CLILoginState{
				MFAVerifiedAt: sql.NullTime{Time: verifiedAt, Valid: true},
				MFAExempt:     tt.exempt,
			}

			env.handler.cliStampMFAVerification(session, record)

			stored := env.storedSession(t, session.ID)
			require.NotNil(t, stored.MFAVerifiedAt)
			if tt.exempt {
				require.Nil(t, stored.RecentStepUpAt)
				require.Nil(t, session.RecentStepUpAt)
				return
			}
			require.NotNil(t, stored.RecentStepUpAt)
			require.WithinDuration(t, verifiedAt, *stored.RecentStepUpAt, time.Millisecond)
		})
	}
}

func TestMarkCLILoginExempt(t *testing.T) {
	env := newStepUpTestEnv(t)
	for _, tt := range []struct {
		state  string
		exempt bool
	}{
		{state: "verified-state", exempt: false},
		{state: "exempt-state", exempt: true},
	} {
		require.NoError(t, env.database.CreateCLILoginState(db.NewCLILogin{State: tt.state}))
		bound, err := env.database.BindCLILoginBrowser(tt.state, "binding-"+tt.state, "user@example.com", "User")
		require.NoError(t, err)
		require.True(t, bound)
		if tt.exempt {
			require.NoError(t, env.database.MarkCLILoginExempt(tt.state))
		} else {
			require.NoError(t, env.database.MarkCLILoginVerified(tt.state, nil))
		}
		record, err := env.database.GetCLILoginState(tt.state)
		require.NoError(t, err)
		require.NotNil(t, record)
		require.True(t, record.MFAVerifiedAt.Valid)
		require.Equal(t, tt.exempt, record.MFAExempt)
	}
}
