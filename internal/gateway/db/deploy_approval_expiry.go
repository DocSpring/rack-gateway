package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ExpiredDeployApproval is a deploy approval request the expiry sweep marked expired.
type ExpiredDeployApproval struct {
	ID             int64
	PublicID       string
	App            string
	GitCommitHash  string
	PreviousStatus string
}

// ExpireDeployApprovalRequests marks requests expired: approvals whose window has passed, and pending requests
// created more than pendingMaxAge ago (when pendingMaxAge is positive). Expired requests no longer count as open,
// so they don't block a new request for the same commit, token and app.
//
// Each expiry's audit event, entry(e), is appended in the same transaction, so a request is never expired without
// its event: when an append fails, nothing is expired and the error wraps ErrAuditAppend. It returns the requests
// it expired and their stored events.
func (d *Database) ExpireDeployApprovalRequests(
	pendingMaxAge time.Duration,
	entry func(ExpiredDeployApproval) *AuditLog,
) ([]ExpiredDeployApproval, []*AuditLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	tx, err := d.beginAuditTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The chain lock first: every audit append takes it before touching any row its event references, so taking
	// the requests' row locks under it can't deadlock with one.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockAuditChain); err != nil {
		return nil, nil, fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}
	expired, err := d.expireDeployApprovalRows(ctx, tx, pendingMaxAge)
	if err != nil {
		return nil, nil, err
	}
	events := make([]*AuditLog, 0, len(expired))
	for _, e := range expired {
		event := entry(e)
		if err := appendAuditLogTx(ctx, tx, event); err != nil {
			return nil, nil, fmt.Errorf("%w for expired deploy approval %d: %w", ErrAuditAppend, e.ID, err)
		}
		events = append(events, event)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit deploy approval expiry: %w", err)
	}
	return expired, events, nil
}

func (d *Database) expireDeployApprovalRows(
	ctx context.Context,
	tx *sql.Tx,
	pendingMaxAge time.Duration,
) ([]ExpiredDeployApproval, error) {
	// The zero time matches no pending request.
	var pendingCutoff time.Time
	if pendingMaxAge > 0 {
		pendingCutoff = time.Now().Add(-pendingMaxAge)
	}
	rows, err := tx.QueryContext(ctx, d.rebind(`WITH expiring AS (
			SELECT id, status FROM deploy_approval_requests
			WHERE (status = ? AND approval_expires_at IS NOT NULL AND approval_expires_at <= NOW())
			   OR (status = ? AND created_at <= ?)
			FOR UPDATE SKIP LOCKED
		)
		UPDATE deploy_approval_requests r SET status = ?, updated_at = NOW()
		FROM expiring WHERE r.id = expiring.id
		RETURNING r.id, r.public_id, r.app, r.git_commit_hash, expiring.status`),
		DeployApprovalRequestStatusApproved,
		DeployApprovalRequestStatusPending, pendingCutoff,
		DeployApprovalRequestStatusExpired,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to expire deploy approval requests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var expired []ExpiredDeployApproval
	for rows.Next() {
		var e ExpiredDeployApproval
		if err := rows.Scan(&e.ID, &e.PublicID, &e.App, &e.GitCommitHash, &e.PreviousStatus); err != nil {
			return nil, fmt.Errorf("failed to read expired deploy approval request: %w", err)
		}
		expired = append(expired, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to expire deploy approval requests: %w", err)
	}
	return expired, nil
}
