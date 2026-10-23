package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A user's first sign-in binds their account. Another account signing in with the same email, such as the holder
// of a reassigned address, is refused until an admin resets the user's sign-in.
func TestWebLoginRefusesAnotherAccountWithTheUsersEmail(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("dev@example.com", "Dev", []string{"viewer"})
	require.NoError(t, err)
	oauth := &fakeOAuth{resp: &auth.LoginResponse{Email: "dev@example.com", Name: "Dev", Subject: "first"}}
	sessions := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessions, nil, nil, nil,
		audit.NewLogger(database))
	login := func() *http.Response {
		c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/callback?code=abc&state=state")
		c.Request.AddCookie(testCookie(webOAuthStateCookie, "state"))
		handler.WebLoginCallback(c)
		return w.Result()
	}

	require.NotNil(t, findCookie(login(), "session_token"), "the first account signs in and is bound")

	oauth.resp = &auth.LoginResponse{Email: "dev@example.com", Name: "Dev", Subject: "second"}
	res := login()
	require.Equal(t, WebLoginErrorRoute+"?error=account_mismatch", res.Header.Get("Location"))
	require.Nil(t, findCookie(res, "session_token"))

	require.NoError(t, database.ResetUserSignInAccount(user.ID, nil))
	require.NotNil(t, findCookie(login(), "session_token"), "after a reset, the next account is bound")
	require.ErrorIs(t, database.BindUserSignInAccount(user.ID, "first"), db.ErrSignInAccountMismatch)
}

func TestCLILoginRefusesAnotherAccountWithTheUsersEmail(t *testing.T) {
	e := newLoopbackEnv(t, false)
	user, err := e.database.GetUser("user@example.com")
	require.NoError(t, err)
	require.NoError(t, e.database.BindUserSignInAccount(user.ID, "someone-else"))
	e.send(t, http.MethodPost, "/api/v1/auth/cli/start", validStartRequest())
	state := e.oauth.lastState

	res := e.send(t, http.MethodGet, "/api/v1/auth/cli/callback?code=google-code&state="+state, nil)
	require.Equal(t, "account_mismatch", loopbackError(t, res))
	require.Nil(t, findCookie(res, cliLoginCookie), "the browser isn't bound to the login")
}
