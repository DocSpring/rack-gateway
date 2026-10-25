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

	waiting := make(chan struct{})
	acquired := make(chan func())
	go func() {
		unlock, err := Lock(path, func() { close(waiting) })
		require.NoError(t, err)
		acquired <- unlock
	}()

	<-waiting
	select {
	case <-acquired:
		t.Fatal("the second Lock took the lock while the first held it")
	case <-time.After(200 * time.Millisecond):
	}
	unlockFirst()
	unlockSecond := <-acquired
	unlockSecond()

	unlockThird, err := Lock(path, func() { t.Error("the lock was released") })
	require.NoError(t, err)
	unlockThird()
}
