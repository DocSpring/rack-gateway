package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

var (
	stateOwner        = oauthStateOwner{email: "admin@example.com", sessionID: 1}
	otherUser         = oauthStateOwner{email: "other@example.com", sessionID: 2}
	sameUserOtherTab  = oauthStateOwner{email: "admin@example.com", sessionID: 3}
	sameUserAPIClient = oauthStateOwner{email: "admin@example.com"}
)

func TestOAuthStateStoreStateIsSingleUse(t *testing.T) {
	store := newOAuthStateStore(time.Minute)
	store.Put("state-1", stateOwner)

	require.True(t, store.Take("state-1", stateOwner))
	require.False(t, store.Take("state-1", stateOwner))
	require.False(t, store.Take("never-issued", stateOwner))
}

func TestOAuthStateStoreOnlyTheOwnerCanTakeAState(t *testing.T) {
	for _, other := range []oauthStateOwner{otherUser, sameUserOtherTab, sameUserAPIClient} {
		store := newOAuthStateStore(time.Minute)
		store.Put("state-1", stateOwner)

		require.False(t, store.Take("state-1", other), other)
		// Sending the state back used it up, so not even its owner can use it now.
		require.False(t, store.Take("state-1", stateOwner), other)
	}
}

func TestOAuthStateStoreStatesExpire(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := newOAuthStateStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	store.Put("fresh", stateOwner)
	store.Put("stale", stateOwner)
	now = now.Add(9 * time.Minute)
	require.True(t, store.Take("fresh", stateOwner))

	now = now.Add(time.Minute)
	require.False(t, store.Take("stale", stateOwner))
}

func TestOAuthStateStorePrunesExpiredStates(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := newOAuthStateStore(time.Minute)
	store.now = func() time.Time { return now }
	for i := range 100 {
		store.Put(fmt.Sprintf("abandoned-%d", i), stateOwner)
	}

	now = now.Add(time.Minute)
	store.Put("current", stateOwner)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.states, 1)
}

// Many admins' requests use the store at once. Run with -race: an unlocked map fails here.
func TestOAuthStateStoreConcurrentUse(t *testing.T) {
	store := newOAuthStateStore(time.Minute)
	var wg sync.WaitGroup
	results := make([]bool, 200)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner := oauthStateOwner{email: fmt.Sprintf("user-%d@example.com", i), sessionID: int64(i)}
			state := fmt.Sprintf("state-%d", i)
			store.Put(state, owner)
			results[i] = store.Take(state, owner)
		}()
	}
	wg.Wait()

	for i, taken := range results {
		require.True(t, taken, "state %d", i)
	}
}

// slackOAuthContext is a request for the Slack OAuth routes from email's browser session sessionID.
func slackOAuthContext(target, email string, sessionID int64) (*gin.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email:   email,
		Session: &db.UserSession{ID: sessionID},
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", email)
	return c, w
}

// startSlackOAuth starts connecting Slack as email's session sessionID, and returns the state Slack will send back.
func startSlackOAuth(t *testing.T, handler *AdminHandler, email string, sessionID int64) string {
	t.Helper()
	c, w := slackOAuthContext("/api/v1/integrations/slack/oauth/authorize", email, sessionID)
	handler.SlackOAuthAuthorizeHandler(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var result map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	authURL, err := url.Parse(result["authorization_url"])
	require.NoError(t, err)
	state := authURL.Query().Get("state")
	require.NotEmpty(t, state)
	return state
}

// A callback must come from the session that started the flow: anyone else following a link with its state (and
// a code for their own workspace) is refused before the code is exchanged.
func TestSlackOAuthCallbackRefusesAStateFromAnotherSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &AdminHandler{
		rbac:   newAllowAllRBAC(),
		config: &config.Config{SlackClientID: "client-id", SlackClientSecret: "client-pw", Domain: "gw.example.com"},
	}

	callers := []struct {
		email     string
		sessionID int64
	}{
		{"other@example.com", 1},
		{"admin@example.com", 2},
	}
	for _, caller := range callers {
		state := startSlackOAuth(t, handler, "admin@example.com", 1)

		c, w := slackOAuthContext(
			"/api/v1/integrations/slack/oauth/callback?code=attacker-code&state="+url.QueryEscape(state),
			caller.email,
			caller.sessionID,
		)
		handler.SlackOAuthCallbackHandler(c)

		require.Equal(t, http.StatusBadRequest, w.Code, caller)
		require.Contains(t, w.Body.String(), "invalid state token")
	}
}
