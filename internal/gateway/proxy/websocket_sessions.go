package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// maxWebSocketSessionsPerPrincipal caps the exec and log sessions one user or API token can have open at once. Each
// session holds two connections with their buffers and three goroutines in the gateway, and a connection on the
// rack, so without a cap one caller could open sessions until the gateway ran out of memory. A person rarely follows
// more than a few log streams or shells at once, and a deploy streams one build's logs.
const maxWebSocketSessionsPerPrincipal = 10

// webSocketSessions counts the open sessions per principal. A gateway serves its rack from a single instance, so an
// in-memory count is the whole count.
var webSocketSessions = &sessionLimiter{limit: maxWebSocketSessionsPerPrincipal}

// sessionLimiter counts open sessions per key and refuses one past the limit.
type sessionLimiter struct {
	mu    sync.Mutex
	limit int
	open  map[string]int
}

// acquire counts a new session for key. It returns false, counting nothing, when key already has limit open.
func (l *sessionLimiter) acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[key] >= l.limit {
		return false
	}
	if l.open == nil {
		l.open = make(map[string]int)
	}
	l.open[key]++
	return true
}

// release ends a session that acquire counted.
func (l *sessionLimiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[key] <= 1 {
		delete(l.open, key)
		return
	}
	l.open[key]--
}

// sessionPrincipal is who a session counts against: an API token by its ID (each token gets its own allowance,
// separate from its creator's), a user by email.
func sessionPrincipal(authUser *auth.User) string {
	switch {
	case authUser == nil:
		return ""
	case authUser.TokenID != nil:
		return fmt.Sprintf("token:%d", *authUser.TokenID)
	default:
		return "user:" + strings.ToLower(authUser.Email)
	}
}

// acquireWebSocketSession counts a new session against the caller. Past the limit it answers 429 and returns false;
// otherwise the caller must call the returned release when the session ends.
func acquireWebSocketSession(w http.ResponseWriter, authUser *auth.User) (release func(), ok bool) {
	principal := sessionPrincipal(authUser)
	if !webSocketSessions.acquire(principal) {
		msg := fmt.Sprintf(
			"too many open exec and log sessions: at most %d at once; close one and try again", webSocketSessions.limit,
		)
		http.Error(w, msg, http.StatusTooManyRequests)
		return nil, false
	}
	return func() { webSocketSessions.release(principal) }, true
}
