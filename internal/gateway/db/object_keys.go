package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrObjectUploadInProgress is returned by ClaimObjectForBuild when the archive's upload hasn't finished, or its
// outcome is unknown.
var ErrObjectUploadInProgress = errors.New("the archive's upload hasn't finished")

// ClaimObjectUpload claims an object key for an upload before it's forwarded, and reports whether it got it. A
// key is claimed once and never released (see the rack_object_keys migration): a second upload to it, from anyone,
// is refused.
func (d *Database) ClaimObjectUpload(app, key string) (bool, error) {
	res, err := d.exec(`INSERT INTO rack_object_keys (app, object_key) VALUES (?, ?)
		ON CONFLICT (app, object_key) DO NOTHING`, app, key)
	if err != nil {
		return false, fmt.Errorf("failed to claim object key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to claim object key: %w", err)
	}
	return rows == 1, nil
}

// MarkObjectUploaded records that the rack stored the upload that claimed the key.
func (d *Database) MarkObjectUploaded(app, key string) error {
	_, err := d.exec(`UPDATE rack_object_keys SET uploaded_at = COALESCE(uploaded_at, NOW())
		WHERE app = ? AND object_key = ?`, app, key)
	if err != nil {
		return fmt.Errorf("failed to record object upload: %w", err)
	}
	return nil
}

// ClaimObjectForBuild claims the key of an archive the gateway is about to check for a build, so it can't be
// uploaded again afterwards. A key the gateway hasn't seen uploaded (an object stored before keys were claimed) is
// claimed as uploaded. It returns ErrObjectUploadInProgress when the key's upload hasn't finished: the gateway
// would check one archive and the rack build another.
func (d *Database) ClaimObjectForBuild(app, key string) error {
	var uploadedAt sql.NullTime
	err := d.queryRow(`INSERT INTO rack_object_keys (app, object_key, uploaded_at) VALUES (?, ?, NOW())
		ON CONFLICT (app, object_key) DO UPDATE SET object_key = rack_object_keys.object_key
		RETURNING uploaded_at`, app, key).Scan(&uploadedAt)
	if err != nil {
		return fmt.Errorf("failed to claim the build's archive: %w", err)
	}
	if !uploadedAt.Valid {
		return ErrObjectUploadInProgress
	}
	return nil
}
