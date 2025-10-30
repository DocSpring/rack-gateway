package db

import (
	"encoding/json"
	"fmt"
	"time"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

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
	gtwlog.DebugTopicf(
		gtwlog.TopicMFAStepUp,
		"db_update_step_up_before session_id=%d when=%q",
		sessionID,
		when.Format(time.RFC3339),
	)
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

func (d *Database) UpdateSessionMetadata(sessionID int64, metadata map[string]interface{}) error {
	if len(metadata) == 0 {
		return nil
	}

	session, err := d.GetSessionByID(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found")
	}

	existingMeta := make(map[string]interface{})
	if len(session.Metadata) > 0 {
		if err := json.Unmarshal(session.Metadata, &existingMeta); err != nil {
			return fmt.Errorf("failed to parse existing metadata: %w", err)
		}
	}

	for k, v := range metadata {
		existingMeta[k] = v
	}

	metaJSON := marshalJSONMap(existingMeta)

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
