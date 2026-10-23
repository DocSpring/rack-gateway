package handlers

import (
	"context"
	"sync"
)

// auditSearchQueue runs one audit log search (a page of the list, or an export) per user at a time: a search can
// scan every event, holding a database connection while it does. A user's next search waits for their last one, or
// gives up with its request. The zero value is ready to use.
type auditSearchQueue struct {
	mu    sync.Mutex
	users map[string]*auditSearchSlot
}

// auditSearchSlot is one user's turn to search, and how many of their searches are running or waiting for it.
type auditSearchSlot struct {
	turn  chan struct{}
	using int
}

// wait waits for user's turn to search, and returns the function that ends it. It returns ctx's error if ctx ends
// first.
func (q *auditSearchQueue) wait(ctx context.Context, user string) (func(), error) {
	q.mu.Lock()
	if q.users == nil {
		q.users = map[string]*auditSearchSlot{}
	}
	slot := q.users[user]
	if slot == nil {
		slot = &auditSearchSlot{turn: make(chan struct{}, 1)}
		q.users[user] = slot
	}
	slot.using++
	q.mu.Unlock()

	select {
	case slot.turn <- struct{}{}:
		return func() {
			<-slot.turn
			q.leave(user, slot)
		}, nil
	case <-ctx.Done():
		q.leave(user, slot)
		return nil, ctx.Err()
	}
}

// leave forgets user's slot once none of their searches is using it.
func (q *auditSearchQueue) leave(user string, slot *auditSearchSlot) {
	q.mu.Lock()
	defer q.mu.Unlock()
	slot.using--
	if slot.using == 0 {
		delete(q.users, user)
	}
}
