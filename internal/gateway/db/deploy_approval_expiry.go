package db

import (
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
// created more than pendingMaxAge ago (when pendingMaxAge is positive). It returns the requests it expired, so the
// caller can audit them. Expired requests no longer count as open, so they don't block a new request for the same
// commit, token and app.
func (d *Database) ExpireDeployApprovalRequests(pendingMaxAge time.Duration) ([]ExpiredDeployApproval, error) {
	// The zero time matches no pending request.
	var pendingCutoff time.Time
	if pendingMaxAge > 0 {
		pendingCutoff = time.Now().Add(-pendingMaxAge)
	}
	rows, err := d.query(`WITH expiring AS (
			SELECT id, status FROM deploy_approval_requests
			WHERE (status = ? AND approval_expires_at IS NOT NULL AND approval_expires_at <= NOW())
			   OR (status = ? AND created_at <= ?)
			FOR UPDATE SKIP LOCKED
		)
		UPDATE deploy_approval_requests r SET status = ?, updated_at = NOW()
		FROM expiring WHERE r.id = expiring.id
		RETURNING r.id, r.public_id, r.app, r.git_commit_hash, expiring.status`,
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
