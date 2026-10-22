package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func lockWithin(database *gwdb.Database, app string, wait time.Duration) (*gwdb.AppEnvLock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return database.LockAppEnv(ctx, app)
}

// One env change to an app at a time: a second waits for the first to release the lock, or gives up busy.
func TestLockAppEnvAllowsOneChangeToAnAppAtATime(t *testing.T) {
	database := dbtest.NewDatabase(t)

	first, err := lockWithin(database, "web", time.Second)
	require.NoError(t, err)

	_, err = lockWithin(database, "web", 300*time.Millisecond)
	require.ErrorIs(t, err, gwdb.ErrAppEnvLockBusy)

	other, err := lockWithin(database, "worker", time.Second)
	require.NoError(t, err, "another app's env has its own lock")
	other.Release()

	got := make(chan error, 1)
	go func() {
		second, err := lockWithin(database, "web", 5*time.Second)
		second.Release()
		got <- err
	}()
	first.Release()
	first.Release()
	require.NoError(t, <-got, "the lock is free once released")
}

// A waiter doesn't hold a connection, so the holder can still use the database however many changes wait.
func TestLockAppEnvWaitersDontHoldConnections(t *testing.T) {
	database := dbtest.NewDatabase(t)
	held, err := lockWithin(database, "web", time.Second)
	require.NoError(t, err)
	defer held.Release()

	waiters := make(chan error, 20)
	for range 20 {
		go func() {
			_, err := lockWithin(database, "web", 2*time.Second)
			waiters <- err
		}()
	}
	_, err = database.GetUser("nobody@example.com")
	require.NoError(t, err)
	for range 20 {
		require.ErrorIs(t, <-waiters, gwdb.ErrAppEnvLockBusy)
	}
}
