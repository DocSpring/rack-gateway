package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// failureLimitHarness is an auth service allowing each client three failed attempts, with a valid API token.
type failureLimitHarness struct {
	database *db.Database
	svc      *Service
	sessions *SessionManager
	owner    *db.User
	token    string
	tokenID  int64
}

func newFailureLimitHarness(t *testing.T) *failureLimitHarness {
	t.Helper()
	t.Setenv("AUTH_FAILURE_LIMIT_BURST", "3")
	t.Setenv("AUTH_FAILURE_LIMIT_PER_MINUTE", "30")
	database := dbtest.NewDatabase(t)
	owner, err := database.CreateUser("owner@example.com", "Owner", []string{"admin"})
	require.NoError(t, err)
	tokens := token.NewService(database)
	tokenResp, err := tokens.GenerateAPIToken(&token.APITokenRequest{
		Name: "CI", UserID: owner.ID, Permissions: token.DefaultCICDPermissions(),
	})
	require.NoError(t, err)
	sessions := NewSessionManager(database, "test-secret", nil)
	return &failureLimitHarness{
		database: database,
		svc:      NewAuthService(tokens, database, sessions, audit.NewLogger(database)),
		sessions: sessions,
		owner:    owner,
		token:    tokenResp.Token,
		tokenID:  tokenResp.APIToken.ID,
	}
}

// serve sends a request from ip with the authorization header (none when empty), and returns the response.
func (h *failureLimitHarness) serve(ip, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/info", nil)
	req.RemoteAddr = ip + ":51234"
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec
}

// A client that has used up its failed attempts is refused before its credentials are checked, even valid ones
// that haven't authenticated recently, until an attempt comes back (see ratelimit.FailureLimiter). Other clients
// aren't affected.
func TestFailedAttemptsAreLimitedPerClient(t *testing.T) {
	h := newFailureLimitHarness(t)
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusUnauthorized, h.serve("203.0.113.7", "Bearer rgw_not_a_real_token").Code)
	}

	refused := h.serve("203.0.113.7", "Bearer "+h.token)
	require.Equal(t, http.StatusTooManyRequests, refused.Code)
	require.Equal(t, "2", refused.Header().Get("Retry-After"), "one attempt comes back every two seconds")
	require.Equal(t, http.StatusOK, h.serve("203.0.113.8", "Bearer "+h.token).Code)
}

// A credential that authenticated recently, or a session the gateway just created, isn't refused for its
// address's failed attempts: someone else behind the same address may have made them. A known credential that's
// refused is forgotten, and counts as a failed attempt.
func TestAKnownCredentialGetsPastItsAddressesFailedAttempts(t *testing.T) {
	h := newFailureLimitHarness(t)
	require.Equal(t, http.StatusOK, h.serve("203.0.113.7", "Bearer "+h.token).Code)
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusUnauthorized, h.serve("203.0.113.7", "Bearer rgw_not_a_real_token").Code)
	}
	require.Equal(t, http.StatusTooManyRequests, h.serve("203.0.113.7", "Bearer rgw_not_a_real_token").Code)
	require.Equal(t, http.StatusOK, h.serve("203.0.113.7", "Bearer "+h.token).Code, "it authenticated before")

	sessionToken, _, err := h.sessions.CreateSession(h.owner, SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, h.serve("203.0.113.7", "Bearer "+sessionToken).Code, "a new session is known")

	require.NoError(t, h.database.DeleteAPIToken(h.tokenID))
	require.Equal(t, http.StatusUnauthorized, h.serve("203.0.113.7", "Bearer "+h.token).Code, "it's still checked")
	require.Equal(t, http.StatusTooManyRequests, h.serve("203.0.113.7", "Bearer "+h.token).Code)
}

// A known credential's attempts still running count against its own limit: once it's revoked, a burst of
// concurrent requests with it can't all be checked before the first of them fails and it's forgotten.
func TestARevokedKnownCredentialGetsNoMoreConcurrentAttemptsThanItsLimit(t *testing.T) {
	h := newFailureLimitHarness(t)
	require.Equal(t, http.StatusOK, h.serve("203.0.113.7", "Bearer "+h.token).Code)
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusUnauthorized, h.serve("203.0.113.7", "Bearer rgw_not_a_real_token").Code)
	}
	require.NoError(t, h.database.DeleteAPIToken(h.tokenID))

	// Token checks wait while api_tokens is locked, so every attempt that gets past the limit is still running.
	lock, err := h.database.DB().Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Rollback() })
	_, err = lock.Exec("LOCK TABLE api_tokens IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	const sent = 10
	codes := make(chan int, sent)
	for i := 0; i < sent; i++ {
		go func() { codes <- h.serve("203.0.113.7", "Bearer "+h.token).Code }()
	}
	for refused := 0; refused < sent-3; refused++ {
		select {
		case code := <-codes:
			require.Equal(t, http.StatusTooManyRequests, code)
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d requests were refused while the token was checked; the rest were all checked",
				refused, sent)
		}
	}
	require.NoError(t, lock.Rollback())
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusUnauthorized, <-codes)
	}
}

// A request without credentials isn't a failed attempt: a browser that isn't signed in yet can't lock itself out.
func TestRequestsWithoutCredentialsArentFailedAttempts(t *testing.T) {
	h := newFailureLimitHarness(t)
	for i := 0; i < 10; i++ {
		require.Equal(t, http.StatusUnauthorized, h.serve("203.0.113.7", "").Code)
	}
	require.Equal(t, http.StatusOK, h.serve("203.0.113.7", "Bearer "+h.token).Code)
}

// Refused API tokens are audited as throttled denials: a client's first is written at once, and its repeats are
// counted rather than each appended to the hash chain.
func TestRefusedTokensAreAuditedOncePerRun(t *testing.T) {
	h := newFailureLimitHarness(t)
	for i := 0; i < 3; i++ {
		h.serve("203.0.113.7", "Bearer rgw_not_a_real_token")
	}
	h.serve("203.0.113.7", "Bearer rgw_not_a_real_token") // refused before the token is checked

	logs, err := h.database.GetAuditLogs("", time.Time{}, 20)
	require.NoError(t, err)
	var counts []int
	for _, entry := range logs {
		if entry.Action == "token.validate" {
			require.Equal(t, audit.StatusDenied, entry.Status)
			counts = append(counts, entry.EventCount)
		}
	}
	require.Equal(t, []int{1}, counts)
}
