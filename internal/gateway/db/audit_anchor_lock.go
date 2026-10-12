package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAuditAnchorLockBusy is returned by WithAuditAnchorLock when another anchor write holds the lock.
var ErrAuditAnchorLockBusy = errors.New("another audit anchor write is in progress")

// AuditAnchorTx is the transaction that holds the audit anchor lock.
type AuditAnchorTx struct {
	tx *sql.Tx
}

// AuditChainHead reads the chain head in the lock's transaction, so it doesn't need a second connection.
func (a *AuditAnchorTx) AuditChainHead(ctx context.Context) (AuditChainHead, error) {
	return auditChainHead(ctx, a.tx)
}

// Now returns the database's clock (when the lock's transaction started). River times jobs by the same clock, so
// an anchor named by it lands in the hour verification expects, whatever the clock of the host running it says.
func (a *AuditAnchorTx) Now(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := a.tx.QueryRowContext(ctx, "SELECT now()").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("failed to read the database clock: %w", err)
	}
	return now, nil
}

// LastIndexWrittenBy returns the chain index of the latest event whose timestamp is at or before t, and whether
// there is one. The anchor writer uses it to check that an anchor already in the bucket covers the events that
// existed when S3 stored it.
func (a *AuditAnchorTx) LastIndexWrittenBy(ctx context.Context, t time.Time) (int64, bool, error) {
	var index int64
	err := a.tx.QueryRowContext(ctx, `
		SELECT chain_index FROM audit.audit_event
		WHERE chain_index >= 0 AND timestamp <= $1
		ORDER BY chain_index DESC
		LIMIT 1`, t).Scan(&index)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to read the audit chain: %w", err)
	}
	return index, true, nil
}

// WithAuditAnchorLock runs fn while holding the audit anchor lock, so anchor writes (the periodic job on every
// gateway instance and `rack-gateway-api write-anchor`) never overlap: each one sees the anchor written before
// it, and only one of them writes an hour's anchor. It doesn't wait: when another write holds the lock it returns
// ErrAuditAnchorLockBusy. The lock is released when fn returns.
func (d *Database) WithAuditAnchorLock(ctx context.Context, fn func(context.Context, *AuditAnchorTx) error) error {
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin audit anchor lock tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", AdvisoryLockAuditAnchor).Scan(&locked)
	if err != nil {
		return fmt.Errorf("failed to take the audit anchor lock: %w", err)
	}
	if !locked {
		return ErrAuditAnchorLockBusy
	}
	return fn(ctx, &AuditAnchorTx{tx: tx})
}
