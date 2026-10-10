package db

import "time"

// SetDeployApprovalExpiryForTest moves an approval's window, e.g. to make it lapse in the middle of a deploy.
func (d *Database) SetDeployApprovalExpiryForTest(id int64, expiresAt time.Time) error {
	_, err := d.exec(`UPDATE deploy_approval_requests SET approval_expires_at = ? WHERE id = ?`, expiresAt, id)
	return err
}
