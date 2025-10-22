package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *Database) CreateTrustedDevice(userID int64, deviceID string, tokenHash string, expiresAt time.Time, ip string, uaHash string, metadata map[string]interface{}) (*TrustedDevice, error) {
	var id int64
	var createdAt, updatedAt time.Time
	var meta interface{}
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal trusted device metadata: %w", err)
		}
		meta = string(b)
	}
	query := `
        INSERT INTO trusted_devices (user_id, device_id, token_hash, expires_at, ip_first, ip_last, user_agent_hash, metadata)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING id, created_at, updated_at
    `
	if err := d.queryRow(query, userID, deviceID, tokenHash, expiresAt, nullableIP(ip), nullableIP(ip), nullableString(uaHash, 128), meta).Scan(&id, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("failed to create trusted device: %w", err)
	}
	return &TrustedDevice{
		ID:            id,
		UserID:        userID,
		DeviceID:      deviceID,
		TokenHash:     tokenHash,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ExpiresAt:     expiresAt,
		LastUsedAt:    createdAt,
		IPFirst:       strings.TrimSpace(ip),
		IPLast:        strings.TrimSpace(ip),
		UserAgentHash: strings.TrimSpace(uaHash),
	}, nil
}

func (d *Database) TouchTrustedDevice(id int64, ip string) error {
	_, err := d.exec("UPDATE trusted_devices SET last_used_at = NOW(), ip_last = COALESCE(?, ip_last), updated_at = NOW() WHERE id = ?", nullableIP(ip), id)
	if err != nil {
		return fmt.Errorf("failed to update trusted device usage: %w", err)
	}
	return nil
}

func (d *Database) GetTrustedDeviceByHash(hash string) (*TrustedDevice, error) {
	query := `
        SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata
        FROM trusted_devices WHERE token_hash = ?
    `
	device, err := scanTrustedDevice(d.queryRow(query, hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trusted device: %w", err)
	}
	return device, nil
}

func (d *Database) GetTrustedDeviceByID(id int64) (*TrustedDevice, error) {
	query := `
        SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata
        FROM trusted_devices WHERE id = ?
    `
	device, err := scanTrustedDevice(d.queryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get trusted device: %w", err)
	}
	return device, nil
}

func (d *Database) RevokeTrustedDevice(id int64, reason string) error {
	_, err := d.exec("UPDATE trusted_devices SET revoked_at = NOW(), revoked_reason = ?, updated_at = NOW() WHERE id = ? AND revoked_at IS NULL", nullableString(reason, 255), id)
	if err != nil {
		return fmt.Errorf("failed to revoke trusted device: %w", err)
	}
	return nil
}

func (d *Database) ListTrustedDevices(userID int64) ([]*TrustedDevice, error) {
	rows, err := d.query(`SELECT id, user_id, device_id, token_hash, created_at, updated_at, expires_at, last_used_at, ip_first, ip_last, user_agent_hash, revoked_at, revoked_reason, metadata FROM trusted_devices WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trusted devices: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var devices []*TrustedDevice
	for rows.Next() {
		device, err := scanTrustedDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan trusted device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate trusted devices: %w", err)
	}
	return devices, nil
}
