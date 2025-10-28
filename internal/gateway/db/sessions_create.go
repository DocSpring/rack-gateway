package db

import (
	"fmt"
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
		INSERT INTO user_sessions (user_id, token_hash, expires_at, channel, device_id, device_name, ip_address, user_agent, metadata, device_metadata)
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
