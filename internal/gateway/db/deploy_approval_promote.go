package db

import (
	"fmt"
	"time"
)

// promoteClaimTimeout is how long a promote claim lasts. It only matters for a promote that may still be running
// on the rack (the gateway timed out or stopped mid-promote): a promote the rack refused releases its claim.
const promoteClaimTimeout = 15 * time.Minute

// ClaimDeployApprovalPromote atomically claims an approved, unexpired request for one promote, so two concurrent
// promotes with the same approval can't both be forwarded. It reports whether this caller got the claim.
func (d *Database) ClaimDeployApprovalPromote(id int64) (bool, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests SET promote_started_at = NOW(), updated_at = NOW()
         WHERE id = ? AND status = ?
           AND (approval_expires_at IS NULL OR approval_expires_at > NOW())
           AND (promote_started_at IS NULL OR promote_started_at < NOW() - make_interval(mins => ?))`,
		id, DeployApprovalRequestStatusApproved, int(promoteClaimTimeout/time.Minute),
	)
	if err != nil {
		return false, fmt.Errorf("failed to claim deploy approval for promote: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to claim deploy approval for promote: %w", err)
	}
	return rows == 1, nil
}

// ReleaseDeployApprovalPromote releases the promote claim after a promote failed, so it can be retried (also after
// the approval is extended, if it expired meanwhile). A promote that succeeded marked the approval deployed, which
// this leaves alone.
func (d *Database) ReleaseDeployApprovalPromote(id int64) error {
	if _, err := d.exec(
		`UPDATE deploy_approval_requests SET promote_started_at = NULL, updated_at = NOW()
         WHERE id = ? AND status <> ?`,
		id, DeployApprovalRequestStatusDeployed,
	); err != nil {
		return fmt.Errorf("failed to release deploy approval promote claim: %w", err)
	}
	return nil
}
