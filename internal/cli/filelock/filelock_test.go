package filelock

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A second Lock on the same file waits, saying so, until the first is released. (Each Lock opens the file again,
// so two in one process conflict as two processes do.)
func TestLockWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "key.lock")
	unlockFirst, err := Lock(path, func() { t.Error("nothing holds the lock yet") })
	require.NoError(t, err)

	type result struct {
		unlock func()
		err    error
	}
	waiting := make(chan struct{})
	acquired := make(chan result, 1)
	go func() {
		unlock, err := Lock(path, func() { close(waiting) })
		acquired <- result{unlock, err}
	}()

	select {
	case second := <-acquired:
		require.NoError(t, second.err)
		t.Fatal("the second Lock took the lock while the first held it")
	case <-waiting:
	}
	select {
	case <-acquired:
		t.Fatal("the second Lock took the lock while the first held it")
	case <-time.After(200 * time.Millisecond):
	}
	unlockFirst()
	second := <-acquired
	require.NoError(t, second.err)
	second.unlock()

	unlockThird, err := Lock(path, func() { t.Error("the lock was released") })
	require.NoError(t, err)
	unlockThird()
}
