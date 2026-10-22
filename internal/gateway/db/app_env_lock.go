package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAppEnvLockBusy is returned by LockAppEnv when another env change to the app held the lock for longer than
// it waits.
var ErrAppEnvLockBusy = errors.New("another env change to this app is in progress")

const (
	appEnvLockWait  = 15 * time.Second
	appEnvLockRetry = 200 * time.Millisecond
)

// AppEnvLock is an app's env write lock, held by an open transaction until Release.
type AppEnvLock struct {
	tx *sql.Tx
}

// Release releases the lock. It is safe to call more than once, and on a nil lock.
func (l *AppEnvLock) Release() {
	if l == nil || l.tx == nil {
		return
	}
	_ = l.tx.Rollback()
	l.tx = nil
}

// LockAppEnv takes the app's env write lock. An env change reads the app's latest env, merges the change into it
// and creates a release with the whole env: two changes made at once would each write back the env they read,
// and the later one would silently undo the earlier one (a protected key's change included). The caller holds
// the lock from reading the env until the rack has created the release.
//
// A waiting caller doesn't hold a database connection: it retries a non-blocking lock until it gets it or
// appEnvLockWait passes, when it returns ErrAppEnvLockBusy. Waiting on the lock would hold a pooled connection
// each, and enough waiters would leave the holder without one to write its audit entries.
func (d *Database) LockAppEnv(ctx context.Context, app string) (*AppEnvLock, error) {
	ctx, cancel := context.WithTimeout(ctx, appEnvLockWait)
	defer cancel()
	for {
		lock, err := d.tryLockAppEnv(ctx, app)
		if err != nil || lock != nil {
			return lock, err
		}
		timer := time.NewTimer(appEnvLockRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ErrAppEnvLockBusy
		case <-timer.C:
		}
	}
}

// tryLockAppEnv returns the lock, or nil when another transaction holds it. The lock's transaction isn't tied to
// ctx (which only bounds the wait): the caller releases it.
func (d *Database) tryLockAppEnv(ctx context.Context, app string) (*AppEnvLock, error) {
	tx, err := d.db.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin the app env lock tx: %w", err)
	}
	var locked bool
	err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1, hashtext($2))", AdvisoryLockAppEnv, app).
		Scan(&locked)
	if err != nil {
		_ = tx.Rollback()
		if ctx.Err() != nil {
			return nil, ErrAppEnvLockBusy
		}
		return nil, fmt.Errorf("failed to take the app env lock: %w", err)
	}
	if !locked {
		_ = tx.Rollback()
		return nil, nil
	}
	return &AppEnvLock{tx: tx}, nil
}
