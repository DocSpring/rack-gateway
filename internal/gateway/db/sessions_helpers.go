package db

import (
	"database/sql"
	"encoding/json"
	"net/netip"
	"strings"
)

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
