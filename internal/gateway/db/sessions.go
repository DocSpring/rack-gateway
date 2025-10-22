package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
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

func (d *Database) getUserSessionWithUser(where string, args ...interface{}) (*UserSession, *User, error) {
	query := fmt.Sprintf(`
		SELECT us.id, us.user_id, us.token_hash, us.created_at, us.updated_at, us.last_seen_at, us.expires_at, us.channel,
		       us.device_id, us.device_name, us.mfa_verified_at, us.recent_step_up_at, us.trusted_device_id,
		       us.ip_address, us.user_agent, us.revoked_at, us.revoked_by_user_id, us.metadata, us.device_metadata,
		       u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended, u.mfa_enrolled,
		       u.mfa_enforced_at, u.preferred_mfa_method, u.locked_at, u.locked_reason, u.locked_by_user_id,
		       lbu.email, lbu.name
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

// GetUserSessionByHash retrieves a session by hashed token value.
func (d *Database) GetUserSessionByHash(tokenHash string) (*UserSession, error) {
	return d.getUserSession("token_hash = ?", tokenHash)
}

// GetUserSessionWithUserByHash retrieves a session and associated user by hashed token value.
func (d *Database) GetUserSessionWithUserByHash(tokenHash string) (*UserSession, *User, error) {
	return d.getUserSessionWithUser("us.token_hash = ?", tokenHash)
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
	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "db_update_step_up_before session_id=%d when=%q", sessionID, when.Format(time.RFC3339))
	res, err := d.exec(
		"UPDATE user_sessions SET recent_step_up_at = ?, updated_at = NOW() WHERE id = ?",
		when,
		sessionID,
	)
	if err != nil {
		gtwlog.Errorf("db_update_step_up_failed session_id=%d error=%q", sessionID, err.Error())
		return fmt.Errorf("failed to update session step-up timestamp: %w", err)
	}
	rows, _ := res.RowsAffected()
	gtwlog.DebugTopicf(gtwlog.TopicMFAStepUp, "db_update_step_up_after session_id=%d rows_affected=%d", sessionID, rows)
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

// sessionScanner holds nullable database fields for scanning session rows.
type sessionScanner struct {
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
}

// scanSessionRow scans a database row into a UserSession using nullable intermediaries.
// It handles the conversion of sql.Null* types to their corresponding Go types.
func scanSessionRow(
	session *UserSession,
	scanner interface {
		Scan(dest ...interface{}) error
	},
	includeRevocation bool,
) error {
	s := &sessionScanner{}

	var scanArgs []interface{}
	if includeRevocation {
		scanArgs = []interface{}{
			&session.ID, &session.UserID, &session.TokenHash,
			&session.CreatedAt, &session.UpdatedAt, &session.LastSeenAt, &session.ExpiresAt,
			&session.Channel, &s.deviceID, &s.deviceName,
			&s.mfaVerified, &s.recentStep, &s.trustedID,
			&s.ip, &s.ua, &s.revoked, &s.revoker, &s.meta, &s.deviceMeta,
		}
	} else {
		scanArgs = []interface{}{
			&session.ID, &session.UserID, &session.TokenHash,
			&session.CreatedAt, &session.UpdatedAt, &session.LastSeenAt, &session.ExpiresAt,
			&session.Channel, &s.deviceID, &s.deviceName,
			&s.mfaVerified, &s.recentStep, &s.trustedID,
			&s.ip, &s.ua, &s.meta, &s.deviceMeta,
		}
	}

	if err := scanner.Scan(scanArgs...); err != nil {
		return err
	}

	applySessionNullables(session, s)
	return nil
}

// applySessionNullables applies nullable fields from sessionScanner to UserSession.
func applySessionNullables(session *UserSession, s *sessionScanner) {
	if s.deviceID.Valid {
		session.DeviceID = s.deviceID.String
	}
	if s.deviceName.Valid {
		session.DeviceName = s.deviceName.String
	}
	if s.mfaVerified.Valid {
		verified := s.mfaVerified.Time
		session.MFAVerifiedAt = &verified
	}
	if s.recentStep.Valid {
		step := s.recentStep.Time
		session.RecentStepUpAt = &step
	}
	if s.trustedID.Valid {
		id := s.trustedID.Int64
		session.TrustedDeviceID = &id
	}
	if s.ip.Valid {
		session.IPAddress = s.ip.String
	}
	if s.ua.Valid {
		session.UserAgent = s.ua.String
	}
	if s.revoked.Valid {
		revokedAt := s.revoked.Time
		session.RevokedAt = &revokedAt
	}
	if s.revoker.Valid {
		id := s.revoker.Int64
		session.RevokedByUser = &id
	}
	if s.meta.Valid {
		session.Metadata = json.RawMessage(s.meta.String)
	}
	if s.deviceMeta.Valid {
		session.DeviceMetadata = json.RawMessage(s.deviceMeta.String)
	}
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
