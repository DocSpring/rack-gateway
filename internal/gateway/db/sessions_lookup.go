package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

func (d *Database) getUserSession(where string, args ...interface{}) (*UserSession, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, token_hash, created_at, updated_at, last_seen_at, expires_at, channel,
		       device_id, device_name, mfa_verified_at, recent_step_up_at, trusted_device_id,
		       ip_address, user_agent, revoked_at, revoked_by_user_id, metadata, device_metadata
		FROM user_sessions
		WHERE %s
	`, where)

	var session UserSession
	row := d.queryRow(query, args...)
	if err := scanSessionRow(&session, row, true); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user session: %w", err)
	}

	return &session, nil
}

func (d *Database) getUserSessionWithUser(
	where string,
	args ...interface{},
) (*UserSession, *User, error) {
	query := fmt.Sprintf(`
		SELECT us.id, us.user_id, us.token_hash, us.created_at, us.updated_at, us.last_seen_at,
		       us.expires_at, us.channel, us.device_id, us.device_name, us.mfa_verified_at,
		       us.recent_step_up_at, us.trusted_device_id, us.ip_address, us.user_agent,
		       us.revoked_at, us.revoked_by_user_id, us.metadata, us.device_metadata,
		       u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended,
		       u.mfa_enrolled, u.mfa_enforced_at, u.preferred_mfa_method, u.locked_at,
		       u.locked_reason, u.locked_by_user_id, lbu.email, lbu.name
		FROM user_sessions us
		JOIN users u ON u.id = us.user_id
		LEFT JOIN users lbu ON lbu.id = u.locked_by_user_id
		WHERE %s
	`, where)

	var (
		session UserSession
		user    User

		rolesJSON     string
		mfaEnforced   sql.NullTime
		preferred     sql.NullString
		lockedAt      sql.NullTime
		lockedReason  sql.NullString
		lockedBy      sql.NullInt64
		lockedByEmail sql.NullString
		lockedByName  sql.NullString
	)

	s := &sessionScanner{}
	row := d.queryRow(query, args...)
	if err := row.Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.CreatedAt, &session.UpdatedAt,
		&session.LastSeenAt, &session.ExpiresAt, &session.Channel, &s.deviceID, &s.deviceName,
		&s.mfaVerified, &s.recentStep, &s.trustedID, &s.ip, &s.ua, &s.revoked, &s.revoker, &s.meta, &s.deviceMeta,
		&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended,
		&user.MFAEnrolled, &mfaEnforced, &preferred, &lockedAt, &lockedReason, &lockedBy,
		&lockedByEmail, &lockedByName,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to get user session with user: %w", err)
	}

	applySessionNullables(&session, s)

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal user roles: %w", err)
	}
	if mfaEnforced.Valid {
		t := mfaEnforced.Time
		user.MFAEnforcedAt = &t
	}
	if preferred.Valid {
		val := preferred.String
		user.PreferredMFAMethod = &val
	}
	if lockedAt.Valid {
		t := lockedAt.Time
		user.LockedAt = &t
	}
	if lockedReason.Valid {
		user.LockedReason = lockedReason.String
	}
	if lockedBy.Valid {
		id := lockedBy.Int64
		user.LockedByUserID = &id
	}
	if lockedByEmail.Valid {
		user.LockedByEmail = lockedByEmail.String
	}
	if lockedByName.Valid {
		user.LockedByName = lockedByName.String
	}

	return &session, &user, nil
}

// GetUserSessionByHash retrieves a user session by token hash.
func (d *Database) GetUserSessionByHash(tokenHash string) (*UserSession, error) {
	return d.getUserSession("token_hash = ?", tokenHash)
}

// GetUserSessionWithUserByHash retrieves a user session and associated user by token hash.
func (d *Database) GetUserSessionWithUserByHash(tokenHash string) (*UserSession, *User, error) {
	return d.getUserSessionWithUser("us.token_hash = ?", tokenHash)
}

// GetUserSessionByID retrieves a user session by ID.
func (d *Database) GetUserSessionByID(id int64) (*UserSession, error) {
	return d.getUserSession("id = ?", id)
}

// GetSessionByID retrieves a session by ID (alias for GetUserSessionByID).
func (d *Database) GetSessionByID(id int64) (*UserSession, error) {
	return d.GetUserSessionByID(id)
}

// ListActiveSessionsByUser retrieves all active sessions for a specific user.
func (d *Database) ListActiveSessionsByUser(userID int64) ([]*UserSession, error) {
	query := `
		SELECT id, user_id, token_hash, created_at, updated_at, last_seen_at, expires_at, channel,
		       device_id, device_name, mfa_verified_at, recent_step_up_at, trusted_device_id,
		       ip_address, user_agent, metadata, device_metadata
		FROM user_sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY last_seen_at DESC
	`

	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user sessions: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	sessions := []*UserSession{}
	for rows.Next() {
		var sess UserSession
		if err := scanSessionRow(&sess, rows, false); err != nil {
			return nil, fmt.Errorf("failed to scan user session: %w", err)
		}
		sessions = append(sessions, &sess)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}

	return sessions, nil
}
