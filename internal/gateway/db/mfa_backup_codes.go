package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrMFAEnrollmentChanged is returned by IssueEnrollmentBackupCodes when the user's MFA enrollment changed after the
// caller read it: their first factor was confirmed, or their last one deleted.
var ErrMFAEnrollmentChanged = errors.New("the user's MFA enrollment changed")

// ReplaceBackupCodes replaces all backup codes for a user with a new set. It holds the user's row lock, so two
// replacements at once can't both keep their codes: the second deletes the first's.
func (d *Database) ReplaceBackupCodes(userID int64, codeHashes []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction for backup codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.lockUserTx(tx, userID); err != nil {
		return err
	}
	if err := d.replaceBackupCodesTx(tx, userID, codeHashes); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit backup code replacement: %w", err)
	}
	return nil
}

// IssueEnrollmentBackupCodes stores the backup codes an enrollment shows, deciding under the user's row lock. A
// user who isn't enrolled gets codeHashes in place of any codes they had: those verify nothing until a factor is
// confirmed (MarkBackupCodeUsed). An enrolled user keeps their codes, and gets codeHashes only if they have none.
// issued reports whether codeHashes were stored.
//
// wasEnrolled is the enrollment the request was authorized for. If it changed since, nothing is stored and the
// error is ErrMFAEnrollmentChanged: a session that started a first enrollment, and lost its MFA verification when
// another session confirmed the first factor, can't take the new account's backup codes.
func (d *Database) IssueEnrollmentBackupCodes(userID int64, wasEnrolled bool, codeHashes []string) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to start transaction for backup codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	enrolled, err := d.lockUserMFAEnrollmentTx(tx, userID)
	if err != nil {
		return false, err
	}
	if enrolled != wasEnrolled {
		return false, ErrMFAEnrollmentChanged
	}
	if enrolled {
		var existing int
		err := tx.QueryRow(d.rebind("SELECT COUNT(*) FROM mfa_backup_codes WHERE user_id = ?"), userID).Scan(&existing)
		if err != nil {
			return false, fmt.Errorf("failed to count backup codes: %w", err)
		}
		if existing > 0 {
			return false, nil
		}
	}
	if err := d.replaceBackupCodesTx(tx, userID, codeHashes); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit backup codes: %w", err)
	}
	return true, nil
}

// lockUserMFAEnrollmentTx takes the user's row lock in tx and returns whether they're MFA enrolled.
func (d *Database) lockUserMFAEnrollmentTx(tx *sql.Tx, userID int64) (bool, error) {
	var enrolled bool
	err := tx.QueryRow(d.rebind("SELECT mfa_enrolled FROM users WHERE id = ? FOR UPDATE"), userID).Scan(&enrolled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, fmt.Errorf("failed to lock user %d: %w", userID, err)
	}
	return enrolled, nil
}

// replaceBackupCodesTx deletes the user's backup codes and stores codeHashes (ignoring blank ones) in tx.
func (d *Database) replaceBackupCodesTx(tx *sql.Tx, userID int64, codeHashes []string) error {
	if _, err := d.execTx(tx, "DELETE FROM mfa_backup_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to clear backup codes: %w", err)
	}
	placeholders := make([]string, 0, len(codeHashes))
	args := make([]interface{}, 0, len(codeHashes)*2)
	for _, hash := range codeHashes {
		if strings.TrimSpace(hash) == "" {
			continue
		}
		placeholders = append(placeholders, "(?, ?)")
		args = append(args, userID, hash)
	}
	if len(placeholders) == 0 {
		return nil
	}
	query := "INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES " + strings.Join(placeholders, ", ")
	if _, err := d.execTx(tx, query, args...); err != nil {
		return fmt.Errorf("failed to insert backup codes: %w", err)
	}
	return nil
}

// ListBackupCodes returns all backup codes for a user.
func (d *Database) ListBackupCodes(userID int64) ([]*MFABackupCode, error) {
	query := `
		SELECT id, user_id, code_hash, created_at, used_at
		FROM mfa_backup_codes
		WHERE user_id = ?
		ORDER BY created_at
	`
	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup codes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var codes []*MFABackupCode
	for rows.Next() {
		var code MFABackupCode
		var used sql.NullTime
		if err := rows.Scan(&code.ID, &code.UserID, &code.CodeHash, &code.CreatedAt, &used); err != nil {
			return nil, fmt.Errorf("failed to scan backup code: %w", err)
		}
		if used.Valid {
			t := used.Time
			code.UsedAt = &t
		}
		codes = append(codes, &code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate backup codes: %w", err)
	}
	return codes, nil
}

// MarkBackupCodeUsed marks an unused backup code of the user used, and reports whether it did. A user's backup codes
// verify nothing until they're MFA enrolled: an enrollment shows its codes before the factor is confirmed.
func (d *Database) MarkBackupCodeUsed(userID int64, hash string) (bool, error) {
	res, err := d.exec(`UPDATE mfa_backup_codes SET used_at = NOW()
		WHERE user_id = ? AND code_hash = ? AND used_at IS NULL
		  AND EXISTS (SELECT 1 FROM users WHERE id = mfa_backup_codes.user_id AND mfa_enrolled)`,
		userID,
		hash,
	)
	if err != nil {
		return false, fmt.Errorf("failed to mark backup code used: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect backup code update: %w", err)
	}
	return n > 0, nil
}
