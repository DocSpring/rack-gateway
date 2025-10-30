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
func (d *Database) CreateUserSession(
	userID int64,
	tokenHash string,
	expiresAt time.Time,
	channel string,
	deviceID string,
	deviceName string,
	ipAddress string,
	userAgent string,
	metadata map[string]interface{},
	deviceMetadata map[string]interface{},
) (*UserSession, error) {
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
		INSERT INTO user_sessions (
			user_id, token_hash, expires_at, channel, device_id, device_name,
			ip_address, user_agent, metadata, device_metadata
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at, last_seen_at
	`

	if err := d.queryRow(
		query,
		userID,
		tokenHash,
		expiresAt,
		chanVal,
		nullableUUID(deviceID),
		nullableString(strings.TrimSpace(deviceName), 150),
		nullableIP(ipAddress),
		nullableString(sanitizeUserAgent(userAgent), 512),
		metaJSON,
		deviceMetaJSON,
	).Scan(&id, &createdAt, &updatedAt, &lastSeenAt); err != nil {
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

func nullableString(s string, maxLen int) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if maxLen > 0 && len(s) > maxLen {
		return s[:maxLen]
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
