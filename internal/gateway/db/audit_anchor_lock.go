package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
