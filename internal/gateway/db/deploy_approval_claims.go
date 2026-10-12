package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Each step of an approved deploy claims its approval before the gateway forwards it, so two concurrent requests
// under one approval can't both reach the rack: the upload claims the approval's object_url, the build claims its
// one build, and a promote claims the approval for one promote at a time. A step the rack didn't complete releases
// its claim so CI can retry it.

// promoteClaimTimeout is how long a promote claim lasts. It only matters for a promote that may still be running
// on the rack (the gateway timed out or stopped mid-promote): a promote the rack refused releases its claim.
const promoteClaimTimeout = 15 * time.Minute

// ErrDeployApprovalObjectBound is returned when an upload's object already holds a deploy approval request's archive.
var ErrDeployApprovalObjectBound = errors.New("the object already holds a deploy approval request's archive")

// DeployApprovalObjectBound reports whether objectURL holds (or is being uploaded as) any deploy approval request's
// archive. The rack overwrites an object uploaded to the same key again, so the gateway refuses that: the archive
// a build was checked against must be the one the rack builds.
func (d *Database) DeployApprovalObjectBound(objectURL string) (bool, error) {
	var bound bool
	err := d.queryRow(
		`SELECT EXISTS (SELECT 1 FROM deploy_approval_requests WHERE object_url = ?)`, objectURL,
	).Scan(&bound)
	if err != nil {
		return false, fmt.Errorf("failed to check deploy approval archives: %w", err)
	}
	return bound, nil
}

// ClaimDeployApprovalObjectUpload binds objectURL to an approved, unexpired request that has no archive yet, before
// the upload is forwarded. It reports whether this caller got the claim, and returns ErrDeployApprovalObjectBound
// when another request already holds objectURL.
func (d *Database) ClaimDeployApprovalObjectUpload(id int64, objectURL string) (bool, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests SET object_url = ?, updated_at = NOW()
         WHERE id = ? AND status = ? AND object_url IS NULL
           AND (approval_expires_at IS NULL OR approval_expires_at > NOW())`,
		objectURL, id, DeployApprovalRequestStatusApproved,
	)
	if err != nil && isUniqueConstraintViolation(err) {
		return false, ErrDeployApprovalObjectBound
	}
	return claimResult(res, err, "upload")
}

// UpdateDeployApprovalRequestObjectURL records that the upload which claimed objectURL for the request finished.
// It only confirms the claim: a response never binds or replaces an approval's archive.
func (d *Database) UpdateDeployApprovalRequestObjectURL(id int64, objectURL string) error {
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET object_uploaded_at = COALESCE(object_uploaded_at, NOW()), updated_at = NOW()
         WHERE id = ? AND object_url = ?`,
		id, objectURL,
	)
	updated, err := updatedOneRow(res, err)
	if err != nil {
		return fmt.Errorf("failed to record deploy approval upload: %w", err)
	}
	if !updated {
		return fmt.Errorf("%w: no upload of %s was claimed for it", ErrDeployApprovalRequestNotFound, objectURL)
	}
	return nil
}

// ReleaseDeployApprovalObjectUpload releases an upload claim whose upload didn't finish (the rack refused it, or the
// gateway didn't record its response), so CI can upload again. A finished upload stays bound.
func (d *Database) ReleaseDeployApprovalObjectUpload(id int64, objectURL string) error {
	if _, err := d.exec(
		`UPDATE deploy_approval_requests SET object_url = NULL, updated_at = NOW()
         WHERE id = ? AND object_url = ? AND object_uploaded_at IS NULL`,
		id, objectURL,
	); err != nil {
		return fmt.Errorf("failed to release deploy approval upload claim: %w", err)
	}
	return nil
}

// ClaimDeployApprovalBuild claims an approved, unexpired request's one build before the build is forwarded. The
// archive's upload must have finished, or the gateway could check one archive and the rack build another.
func (d *Database) ClaimDeployApprovalBuild(id int64) (bool, error) {
	res, err := d.exec(
		`UPDATE deploy_approval_requests SET build_started_at = NOW(), updated_at = NOW()
         WHERE id = ? AND status = ? AND (approval_expires_at IS NULL OR approval_expires_at > NOW())
           AND object_uploaded_at IS NOT NULL AND build_started_at IS NULL AND build_id IS NULL`,
		id, DeployApprovalRequestStatusApproved,
	)
	return claimResult(res, err, "build")
}

// ReleaseDeployApprovalBuild releases a build claim whose build wasn't recorded (the rack refused it, or the gateway
// didn't get its id back), so CI can build again. A build the rack created without the gateway recording it can't
// be promoted with the approval, which only binds the recorded build's release.
func (d *Database) ReleaseDeployApprovalBuild(id int64) error {
	if _, err := d.exec(
		`UPDATE deploy_approval_requests SET build_started_at = NULL, updated_at = NOW()
         WHERE id = ? AND build_id IS NULL AND build_started_at IS NOT NULL`,
		id,
	); err != nil {
		return fmt.Errorf("failed to release deploy approval build claim: %w", err)
	}
	return nil
}

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
	return claimResult(res, err, "promote")
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

// claimResult reports whether a claim's update claimed its row.
func claimResult(res sql.Result, err error, step string) (bool, error) {
	claimedRow, err := updatedOneRow(res, err)
	if err != nil {
		return false, fmt.Errorf("failed to claim deploy approval for %s: %w", step, err)
	}
	return claimedRow, nil
}

// updatedOneRow reports whether a single-row update changed its row.
func updatedOneRow(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}
