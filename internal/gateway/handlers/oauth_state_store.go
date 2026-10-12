package handlers

import (
	"context"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// slackOAuthStateTTL is how long connecting Slack may take, from the Integrations page's Connect button to Slack
// sending the browser back.
const slackOAuthStateTTL = 10 * time.Minute

// slackOAuthStates holds the states of Slack OAuth flows in progress.
var slackOAuthStates = newOAuthStateStore(slackOAuthStateTTL)

// oauthStateOwner is the user and browser session that started an OAuth flow. Only they can finish it, so a
// callback link carrying someone else's state (and their workspace's code) can't connect a workspace for them.
type oauthStateOwner struct {
	email     string
	sessionID int64
}

// oauthStateOwnerFor is the owner of a flow started or finished by an authenticated request. The session is 0
// for an API token, which has no browser session to finish a flow in.
func oauthStateOwnerFor(ctx context.Context, email string) oauthStateOwner {
	sessionID, _ := auth.GetSessionID(ctx)
	return oauthStateOwner{email: email, sessionID: sessionID}
}

type oauthStateEntry struct {
	owner     oauthStateOwner
	expiresAt time.Time
}

// oauthStateStore holds the states of OAuth flows in progress. A state can be taken once, by the owner it was
// put for, before it expires. Requests use the store concurrently, so every access holds the lock, and expired
// states are pruned on every access so abandoned flows don't pile up.
type oauthStateStore struct {
	mu     sync.Mutex
	ttl    time.Duration
	now    func() time.Time
	states map[string]oauthStateEntry
}

func newOAuthStateStore(ttl time.Duration) *oauthStateStore {
	return &oauthStateStore{ttl: ttl, now: time.Now, states: map[string]oauthStateEntry{}}
}

// Put records state as started by owner.
func (s *oauthStateStore) Put(state string, owner oauthStateOwner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	s.states[state] = oauthStateEntry{owner: owner, expiresAt: now.Add(s.ttl)}
}

// Take reports whether state was put for owner and hasn't expired. The state is used up either way: it was sent
// back once, and whoever sent it, it won't be valid again.
func (s *oauthStateStore) Take(state string, owner oauthStateOwner) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	entry, ok := s.states[state]
	if !ok {
		return false
	}
	delete(s.states, state)
	return entry.owner == owner
}

// pruneLocked removes expired states. The caller holds the lock.
func (s *oauthStateStore) pruneLocked(now time.Time) {
	for state, entry := range s.states {
		if !now.Before(entry.expiresAt) {
			delete(s.states, state)
		}
	}
}
