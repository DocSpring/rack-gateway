package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// CreateUserSession stores a new authenticated session for a user.
func (d *Database) CreateUserSession(userID int64, tokenHash string, expiresAt time.Time, channel string, deviceID string, deviceName string, ipAddress, userAgent string, metadata map[string]interface{}, deviceMetadata map[string]interface{}) (*UserSession, error) {
	if strings.TrimSpace(tokenHash) == "" {
		return nil, fmt.Errorf("token hash is required")
	}
	chanVal := strings.TrimSpace(channel)
	if chanVal == "" {
		chanVal = "web"
	}

	metaJSON := marshalJSONMap(metadata)
	deviceMetaJSON := marshalJSONMap(deviceMetadata)

	var (
		id         int64
		createdAt  time.Time
		updatedAt  time.Time
		lastSeenAt time.Time
	)

	query := `
		INSERT INTO user_sessions (user_id, token_hash, expires_at, channel, device_id, device_name, ip_address, user_agent, metadata, device_metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at, last_seen_at
	`

	if err := d.queryRow(query, userID, tokenHash, expiresAt, chanVal, nullableUUID(deviceID), nullableString(strings.TrimSpace(deviceName), 150), nullableIP(ipAddress), nullableString(sanitizeUserAgent(userAgent), 512), metaJSON, deviceMetaJSON).
		Scan(&id, &createdAt, &updatedAt, &lastSeenAt); err != nil {
		return nil, fmt.Errorf("failed to create user session: %w", err)
	}

	return &UserSession{
		ID:         id,
		UserID:     userID,
		TokenHash:  tokenHash,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		LastSeenAt: lastSeenAt,
		ExpiresAt:  expiresAt,
		Channel:    chanVal,
		DeviceID:   strings.TrimSpace(deviceID),
		DeviceName: strings.TrimSpace(deviceName),
		IPAddress:  strings.TrimSpace(ipAddress),
		UserAgent:  sanitizeUserAgent(userAgent),
	}, nil
}

func (d *Database) getUserSession(where string, args ...interface{}) (*UserSession, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, token_hash, created_at, updated_at, last_seen_at, expires_at, channel, device_id, device_name,
		       mfa_verified_at, recent_step_up_at, trusted_device_id, ip_address, user_agent, revoked_at, revoked_by_user_id, metadata, device_metadata
		FROM user_sessions
		WHERE %s
	`, where)

	var (
		session     UserSession
		deviceID    sql.NullString
		deviceName  sql.NullString
		mfaVerified sql.NullTime
		recentStep  sql.NullTime
		trustedID   sql.NullInt64
		ip          sql.NullString
		ua          sql.NullString
		revoked     sql.NullTime
		revoker     sql.NullInt64
		meta        sql.NullString
		deviceMeta  sql.NullString
	)

	row := d.queryRow(query, args...)
	if err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &session.CreatedAt, &session.UpdatedAt,
		&session.LastSeenAt, &session.ExpiresAt, &session.Channel, &deviceID, &deviceName, &mfaVerified, &recentStep, &trustedID, &ip, &ua, &revoked, &revoker, &meta, &deviceMeta); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user session: %w", err)
	}

	if deviceID.Valid {
		session.DeviceID = deviceID.String
	}
	if deviceName.Valid {
		session.DeviceName = deviceName.String
	}

	if mfaVerified.Valid {
		verified := mfaVerified.Time
		session.MFAVerifiedAt = &verified
	}
	if recentStep.Valid {
		step := recentStep.Time
		session.RecentStepUpAt = &step
	}
	if trustedID.Valid {
		id := trustedID.Int64
		session.TrustedDeviceID = &id
	}
	if ip.Valid {
		session.IPAddress = ip.String
	}
	if ua.Valid {
		session.UserAgent = ua.String
	}
	if revoked.Valid {
		revokedAt := revoked.Time
		session.RevokedAt = &revokedAt
	}
	if revoker.Valid {
		id := revoker.Int64
		session.RevokedByUser = &id
	}
	if meta.Valid {
		session.Metadata = json.RawMessage(meta.String)
	}
	if deviceMeta.Valid {
		session.DeviceMetadata = json.RawMessage(deviceMeta.String)
	}

	return &session, nil
}

// GetUserSessionByHash retrieves a session by hashed token value.
func (d *Database) GetUserSessionByHash(tokenHash string) (*UserSession, error) {
	return d.getUserSession("token_hash = ?", tokenHash)
}

// GetUserSessionByID retrieves a session by identifier.
func (d *Database) GetUserSessionByID(id int64) (*UserSession, error) {
	return d.getUserSession("id = ?", id)
}

// TouchUserSession updates the last_seen_at timestamp and sliding expiration for a session.
func (d *Database) TouchUserSession(id int64, ipAddress, userAgent string, lastSeen, expiresAt time.Time) error {
	_, err := d.exec(
		"UPDATE user_sessions SET last_seen_at = ?, expires_at = ?, updated_at = ?, ip_address = COALESCE(?, ip_address), user_agent = COALESCE(?, user_agent) WHERE id = ?",
		lastSeen,
		expiresAt,
		lastSeen,
		nullableIP(ipAddress),
		nullableString(sanitizeUserAgent(userAgent), 512),
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update session activity: %w", err)
	}
	return nil
}

// RevokeUserSession marks a session as revoked.
func (d *Database) RevokeUserSession(id int64, revokedBy *int64) (bool, error) {
	var revokedByVal interface{}
	if revokedBy != nil {
		revokedByVal = *revokedBy
	}
	res, err := d.exec(
		"UPDATE user_sessions SET revoked_at = NOW(), revoked_by_user_id = ?, updated_at = NOW() WHERE id = ? AND revoked_at IS NULL",
		revokedByVal,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("failed to revoke user session: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect session revoke result: %w", err)
	}
	return rows > 0, nil
}

// RevokeUserSessionByHash marks a session as revoked by its token hash.
func (d *Database) RevokeUserSessionByHash(tokenHash string, revokedBy *int64) (bool, error) {
	var revokedByVal interface{}
	if revokedBy != nil {
		revokedByVal = *revokedBy
	}
	res, err := d.exec(
		"UPDATE user_sessions SET revoked_at = NOW(), revoked_by_user_id = ?, updated_at = NOW() WHERE token_hash = ? AND revoked_at IS NULL",
		revokedByVal,
		tokenHash,
	)
	if err != nil {
		return false, fmt.Errorf("failed to revoke user session by hash: %w", err)
	}
	rowCount, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect revoke result: %w", err)
	}
	return rowCount > 0, nil
}

// RevokeAllUserSessions revokes all active sessions for a user.
func (d *Database) RevokeAllUserSessions(userID int64, revokedBy *int64) (int64, error) {
	var revokedByVal interface{}
	if revokedBy != nil {
		revokedByVal = *revokedBy
	}
	res, err := d.exec(
		"UPDATE user_sessions SET revoked_at = NOW(), revoked_by_user_id = ?, updated_at = NOW() WHERE user_id = ? AND revoked_at IS NULL",
		revokedByVal,
		userID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke user sessions: %w", err)
	}
	rowCount, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to inspect revoke-all result: %w", err)
	}
	return rowCount, nil
}

// ListActiveSessionsByUser returns non-expired, non-revoked sessions for a user.
func (d *Database) ListActiveSessionsByUser(userID int64) ([]*UserSession, error) {
	query := `
		SELECT id, user_id, token_hash, created_at, updated_at, last_seen_at, expires_at, channel, device_id, device_name,
		       mfa_verified_at, recent_step_up_at, trusted_device_id, ip_address, user_agent, metadata, device_metadata
		FROM user_sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY last_seen_at DESC
	`

	rows, err := d.query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user sessions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort cleanup

	sessions := []*UserSession{}
	for rows.Next() {
		var (
			sess        UserSession
			deviceID    sql.NullString
			deviceName  sql.NullString
			mfaVerified sql.NullTime
			recentStep  sql.NullTime
			trustedID   sql.NullInt64
			ip          sql.NullString
			ua          sql.NullString
			meta        sql.NullString
			deviceMeta  sql.NullString
		)
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.CreatedAt, &sess.UpdatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.Channel, &deviceID, &deviceName, &mfaVerified, &recentStep, &trustedID, &ip, &ua, &meta, &deviceMeta); err != nil {
			return nil, fmt.Errorf("failed to scan user session: %w", err)
		}
		if deviceID.Valid {
			sess.DeviceID = deviceID.String
		}
		if deviceName.Valid {
			sess.DeviceName = deviceName.String
		}
		if mfaVerified.Valid {
			verified := mfaVerified.Time
			sess.MFAVerifiedAt = &verified
		}
		if recentStep.Valid {
			step := recentStep.Time
			sess.RecentStepUpAt = &step
		}
		if trustedID.Valid {
			id := trustedID.Int64
			sess.TrustedDeviceID = &id
		}
		if ip.Valid {
			sess.IPAddress = ip.String
		}
		if ua.Valid {
			sess.UserAgent = ua.String
		}
		if meta.Valid {
			sess.Metadata = json.RawMessage(meta.String)
		}
		if deviceMeta.Valid {
			sess.DeviceMetadata = json.RawMessage(deviceMeta.String)
		}
		sessions = append(sessions, &sess)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}

	return sessions, nil
}

func (d *Database) UpdateSessionMFAVerified(sessionID int64, verifiedAt time.Time, trustedDeviceID *int64) error {
	var trusted interface{}
	if trustedDeviceID != nil {
		trusted = *trustedDeviceID
	}
	_, err := d.exec(
		"UPDATE user_sessions SET mfa_verified_at = ?, trusted_device_id = COALESCE(?, trusted_device_id), updated_at = NOW() WHERE id = ?",
		verifiedAt,
		trusted,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to update MFA verification: %w", err)
	}
	return nil
}

func (d *Database) UpdateSessionRecentStepUp(sessionID int64, when time.Time) error {
	_, err := d.exec(
		"UPDATE user_sessions SET recent_step_up_at = ?, updated_at = NOW() WHERE id = ?",
		when,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session step-up timestamp: %w", err)
	}
	return nil
}

func (d *Database) AttachTrustedDeviceToSession(sessionID int64, trustedDeviceID int64) error {
	_, err := d.exec(
		"UPDATE user_sessions SET trusted_device_id = ?, updated_at = NOW() WHERE id = ?",
		trustedDeviceID,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to attach trusted device to session: %w", err)
	}
	return nil
}

func sanitizeUserAgent(ua string) string {
	const maxUserAgentLength = 512
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ""
	}
	if len(ua) > maxUserAgentLength {
		return ua[:maxUserAgentLength]
	}
	return ua
}

func nullableIP(ip string) interface{} {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.String()
}

func nullableString(s string, max int) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if max > 0 && len(s) > max {
		return s[:max]
	}
	return s
}

func marshalJSONMap(m map[string]interface{}) interface{} {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return string(b)
}

func nullableUUID(val string) interface{} {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// GetSessionByID retrieves a session by its ID (alias for GetUserSessionByID for consistency).
func (d *Database) GetSessionByID(id int64) (*UserSession, error) {
	return d.GetUserSessionByID(id)
}

// UpdateSessionMetadata merges new metadata into the existing session metadata.
func (d *Database) UpdateSessionMetadata(sessionID int64, metadata map[string]interface{}) error {
	if len(metadata) == 0 {
		return nil
	}

	// Get current session
	session, err := d.GetSessionByID(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found")
	}

	// Parse existing metadata
	existingMeta := make(map[string]interface{})
	if len(session.Metadata) > 0 {
		if err := json.Unmarshal(session.Metadata, &existingMeta); err != nil {
			return fmt.Errorf("failed to parse existing metadata: %w", err)
		}
	}

	// Merge new metadata
	for k, v := range metadata {
		existingMeta[k] = v
	}

	// Serialize back to JSON
	metaJSON := marshalJSONMap(existingMeta)

	// Update session
	_, err = d.exec(
		"UPDATE user_sessions SET metadata = ?, updated_at = NOW() WHERE id = ?",
		metaJSON,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session metadata: %w", err)
	}
	return nil
}
