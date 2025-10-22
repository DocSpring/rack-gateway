package mfa

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/google/uuid"
)

// TrustedDeviceCookiePayload holds the token and identifier for clients.
type TrustedDeviceCookiePayload struct {
	Token    string
	DeviceID uuid.UUID
	RecordID int64
}

// MintTrustedDevice creates a persistent trusted device record and returns the cookie payload.
func (s *Service) MintTrustedDevice(userID int64, ip, userAgent string) (*TrustedDeviceCookiePayload, error) {
	deviceID := uuid.New()
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate trusted device token: %w", err)
	}
	token := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(tokenBytes)
	tokenHash := hashToken(token)

	expiresAt := time.Now().Add(s.trustedDeviceTTL)
	uaHash := hashUserAgent(userAgent)
	deviceMeta := map[string]interface{}{
		"user_agent": strings.TrimSpace(userAgent),
	}
	device, err := s.db.CreateTrustedDevice(userID, deviceID.String(), tokenHash, expiresAt, ip, uaHash, deviceMeta)
	if err != nil {
		return nil, err
	}
	_ = s.db.TouchTrustedDevice(device.ID, ip)
	payload := &TrustedDeviceCookiePayload{Token: token, DeviceID: deviceID, RecordID: device.ID}
	return payload, nil
}

// ConsumeTrustedDevice validates a trusted-device cookie token and updates its metadata.
func (s *Service) ConsumeTrustedDevice(token string, ip string, userAgent string) (*db.TrustedDevice, error) {
	tokenHash := hashToken(token)
	device, err := s.db.GetTrustedDeviceByHash(tokenHash)
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, fmt.Errorf("trusted device not found")
	}
	if device.RevokedAt != nil {
		return nil, fmt.Errorf("trusted device revoked")
	}
	if time.Now().After(device.ExpiresAt) {
		_ = s.db.RevokeTrustedDevice(device.ID, "expired")
		return nil, fmt.Errorf("trusted device expired")
	}
	if device.UserAgentHash != "" && device.UserAgentHash != hashUserAgent(userAgent) {
		return nil, fmt.Errorf("user agent mismatch")
	}
	if err := s.db.TouchTrustedDevice(device.ID, ip); err != nil {
		return nil, err
	}
	return device, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func hashUserAgent(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(ua)))
	return hex.EncodeToString(sum[:])
}
