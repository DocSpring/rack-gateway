package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LockedUser is a user the gateway locked by itself.
type LockedUser struct {
	ID     int64
	Email  string
	Reason string
}

// LockUserAudited locks the user unless they're already locked. The lock's audit event, entry(user), is appended in
// the same transaction, so the user is never locked without it: when the append fails, nothing is locked and the
// error wraps ErrAuditAppend. It returns the user it locked (nil when they already were) and the stored events.
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
