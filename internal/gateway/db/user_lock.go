package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	// ErrLockOwnAccount is returned by LockUser when an admin tries to lock themselves.
	ErrLockOwnAccount = errors.New("you can't lock your own account")
	// ErrLockLastAdmin is returned by LockUser when the user is the last unlocked admin, and nobody could unlock
	// anyone after the lock.
	ErrLockLastAdmin = errors.New("this is the last unlocked admin")
)

// LockedUser is a user the gateway locked by itself.
type LockedUser struct {
	ID     int64
	Email  string
	Reason string
}

// LockUserAudited locks the user unless they're already locked, and revokes their sessions. The lock's audit event,
// entry(user), is appended in the same transaction, so the user is never locked without it: when the append fails,
// nothing is locked and the error wraps ErrAuditAppend. It returns the user it locked (nil when they already were)
// and the stored events.
func (d *Database) LockUserAudited(
	userID int64,
	reason string,
	entry func(LockedUser) *AuditLog,
) (*LockedUser, []*AuditLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	tx, err := d.beginAuditTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The chain lock first, as every audit append takes it before touching the rows its event references.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockAuditChain); err != nil {
		return nil, nil, fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}
	locked := LockedUser{ID: userID, Reason: reason}
	err = tx.QueryRowContext(ctx, d.rebind(`UPDATE users
		SET locked_at = NOW(), locked_reason = ?, locked_by_user_id = NULL, updated_at = NOW()
		WHERE id = ? AND locked_at IS NULL
		RETURNING email`), nullableString(reason, 255), userID).Scan(&locked.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock user: %w", err)
	}
	if _, err := d.revokeUserSessionsTx(tx, userID, nil); err != nil {
		return nil, nil, err
	}
	event := entry(locked)
	if err := appendAuditLogTx(ctx, tx, event); err != nil {
		return nil, nil, fmt.Errorf("%w for locking user %s: %w", ErrAuditAppend, locked.Email, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit user lock: %w", err)
	}
	return &locked, []*AuditLog{event}, nil
}

// UnlockedAdminEmails returns the emails of the admins who aren't locked, sorted.
func (d *Database) UnlockedAdminEmails() ([]string, error) {
	rows, err := d.query(`SELECT email FROM users
		WHERE roles::jsonb @> '["admin"]' AND locked_at IS NULL
		ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("failed to list admins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("failed to read admin: %w", err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list admins: %w", err)
	}
	return emails, nil
}

// LockUser locks a user account and revokes their sessions, in one transaction. lockedByUserID is the admin who
// locked them, or nil for a lock no admin made. An admin can't lock themselves (ErrLockOwnAccount) or the last
// unlocked admin (ErrLockLastAdmin): only an admin can unlock anyone.
func (d *Database) LockUser(userID int64, reason string, lockedByUserID *int64) error {
	if lockedByUserID != nil && *lockedByUserID == userID {
		return ErrLockOwnAccount
	}
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin user lock: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if lockedByUserID != nil {
		if err := checkLockLeavesAnAdminTx(tx, userID); err != nil {
			return err
		}
	}
	_, err = d.execTx(tx, `
        UPDATE users
        SET locked_at = NOW(), locked_reason = ?, locked_by_user_id = ?, updated_at = NOW()
        WHERE id = ? AND locked_at IS NULL
    `, nullableString(reason, 255), lockedByUserID, userID)
	if err != nil {
		return fmt.Errorf("failed to lock user: %w", err)
	}
	if _, err := d.revokeUserSessionsTx(tx, userID, lockedByUserID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit user lock: %w", err)
	}
	return nil
}

// checkLockLeavesAnAdminTx returns ErrLockLastAdmin when userID is the only unlocked admin. It holds the row locks of
// every unlocked admin until tx ends, so two admins locking each other at once can't both succeed: the second sees
// the first's lock.
func checkLockLeavesAnAdminTx(tx *sql.Tx, userID int64) error {
	rows, err := tx.Query(`SELECT id FROM users WHERE roles::jsonb @> '["admin"]' AND locked_at IS NULL
		ORDER BY id FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("failed to lock the admins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	targetIsAdmin, others := false, 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to read an admin: %w", err)
		}
		if id == userID {
			targetIsAdmin = true
		} else {
			others++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to lock the admins: %w", err)
	}
	if targetIsAdmin && others == 0 {
		return ErrLockLastAdmin
	}
	return nil
}

// revokeUserSessionsTx revokes all of a user's sessions in tx and returns how many it revoked. A lock revokes them
// with it: a session left alive while the user is locked would work again once they're unlocked.
func (d *Database) revokeUserSessionsTx(tx *sql.Tx, userID int64, revokedBy *int64) (int64, error) {
	var revokedByVal interface{}
	if revokedBy != nil {
		revokedByVal = *revokedBy
	}
	res, err := d.execTx(tx, `UPDATE user_sessions SET revoked_at = NOW(), revoked_by_user_id = ?, updated_at = NOW()
		WHERE user_id = ? AND revoked_at IS NULL`, revokedByVal, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke the user's sessions: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to revoke the user's sessions: %w", err)
	}
	return count, nil
}

// UnlockUser unlocks a user account by clearing the lock fields.
func (d *Database) UnlockUser(userID int64, _ int64) error {
	_, err := d.exec(`
        UPDATE users
        SET locked_at = NULL, locked_reason = NULL, locked_by_user_id = NULL, updated_at = NOW()
        WHERE id = ?
    `, userID)
	if err != nil {
		return fmt.Errorf("failed to unlock user: %w", err)
	}
	return nil
}

// IsUserLocked checks if a user account is currently locked
func (d *Database) IsUserLocked(userID int64) (bool, error) {
	var locked bool
	err := d.queryRow(`
        SELECT locked_at IS NOT NULL
        FROM users WHERE id = ?
    `, userID).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("failed to check if user is locked: %w", err)
	}
	return locked, nil
}
