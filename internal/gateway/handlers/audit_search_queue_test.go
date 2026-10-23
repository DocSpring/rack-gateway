package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// One user's searches run one at a time; other users' don't wait for them. A search whose request ends gives up its
// place, and a user with no search running or waiting isn't remembered.
func TestAuditSearchQueue(t *testing.T) {
	var queue auditSearchQueue
	ctx := context.Background()

	done, err := queue.wait(ctx, "alice@example.com")
	require.NoError(t, err)
	other, err := queue.wait(ctx, "bob@example.com")
	require.NoError(t, err)
	other()

	ended, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = queue.wait(ended, "alice@example.com")
	require.ErrorIs(t, err, context.DeadlineExceeded, "alice's second search waits for her first")

	next := make(chan func())
	go func() {
		release, err := queue.wait(ctx, "alice@example.com")
		if err == nil {
			next <- release
		}
	}()
	select {
	case <-next:
		t.Fatal("alice's next search ran while her first was running")
	case <-time.After(50 * time.Millisecond):
	}
	done()
	select {
	case release := <-next:
		release()
	case <-time.After(5 * time.Second):
		t.Fatal("alice's next search didn't run once her first finished")
	}
	require.Empty(t, queue.users)
}
