package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AutoApproveDeployApprovalRequest approves a pending deploy approval request, by internal ID, with no approving
// user: the gateway approves it as it's created (deploy_approval_auto_approve). The audit event entry builds from the
// approved request is appended in the same transaction, so the approval exists only if its record does. A request
// that isn't pending any more is ErrDeployApprovalRequestNotFound. It returns the approved request and the event as
// stored.
func (d *Database) AutoApproveDeployApprovalRequest(
	id int64,
	expiresAt time.Time,
	notes string,
	entry func(*DeployApprovalRequest) *AuditLog,
) (*DeployApprovalRequest, *AuditLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	tx, err := d.beginAuditTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The chain lock first, as in ExpireDeployApprovalRequests: every audit append takes it before touching a row
	// its event references.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockAuditChain); err != nil {
		return nil, nil, fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}
	approved := &DeployApprovalRequest{ID: id, Status: DeployApprovalRequestStatusApproved}
	err = tx.QueryRowContext(ctx, d.rebind(`UPDATE deploy_approval_requests
		SET status = ?, approved_by_user_id = NULL, approved_at = NOW(), approval_expires_at = ?,
		    approval_notes = ?, updated_at = NOW()
		WHERE id = ? AND status = ?
		RETURNING public_id, app, git_commit_hash, approval_expires_at`),
		DeployApprovalRequestStatusApproved, expiresAt, notes, id, DeployApprovalRequestStatusPending,
	).Scan(&approved.PublicID, &approved.App, &approved.GitCommitHash, &approved.ApprovalExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrDeployApprovalRequestNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to auto-approve deploy approval request %d: %w", id, err)
	}

	event := entry(approved)
	if err := appendAuditLogTx(ctx, tx, event); err != nil {
		return nil, nil, fmt.Errorf("%w for auto-approved deploy approval %d: %w", ErrAuditAppend, id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit deploy approval auto-approval: %w", err)
	}
	stored, err := d.GetDeployApprovalRequest(id)
	if err != nil {
		return nil, nil, err
	}
	return stored, event, nil
}
