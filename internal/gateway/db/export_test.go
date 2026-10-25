package db

import "time"

// SetDeployApprovalExpiryForTest moves an approval's window, e.g. to make it lapse in the middle of a deploy.
func (d *Database) SetDeployApprovalExpiryForTest(id int64, expiresAt time.Time) error {
	_, err := d.exec(`UPDATE deploy_approval_requests SET approval_expires_at = ? WHERE id = ?`, expiresAt, id)
	return err
}

func (d *Database) SetDeployApprovalCreatedAtForTest(id int64, createdAt time.Time) error {
	_, err := d.exec(`UPDATE deploy_approval_requests SET created_at = ? WHERE id = ?`, createdAt, id)
	return err
}

// RunMigrationForTest runs the migration file name again, on a database that has run every migration.
func (d *Database) RunMigrationForTest(name string) error {
	sql, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(string(sql))
	return err
}
