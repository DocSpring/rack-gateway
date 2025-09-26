package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpPeriodSeconds = 30
	backupCodeCount   = 10
	backupCodeBytes   = 6 // 12 hex chars
)

// Service orchestrates MFA enrollment, verification, and trusted devices.
type Service struct {
	db               *db.Database
	issuer           string
	trustedDeviceTTL time.Duration
	stepUpWindow     time.Duration
	backupCodePepper []byte
}

// Settings encapsulates runtime MFA configuration loaded from the database.
type Settings struct {
	RequireAllUsers  bool
	TrustedDeviceTTL time.Duration
	StepUpWindow     time.Duration
}

// StartTOTPEnrollmentResult contains the bootstrap payload for the client.
type StartTOTPEnrollmentResult struct {
	MethodID    int64
	Secret      string
	URI         string
	BackupCodes []string
}

// VerificationResult represents the outcome of an MFA verification.
type VerificationResult struct {
	MethodID        int64
	TrustedDeviceID *int64
}

// NewService creates a new MFA service instance.

func NewService(database *db.Database, issuer string, trustedDeviceTTL, stepUpWindow time.Duration, backupCodePepper []byte) (*Service, error) {
	if len(backupCodePepper) == 0 {
		return nil, fmt.Errorf("backup code pepper is required")
	}
	ttl := trustedDeviceTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	window := stepUpWindow
	if window <= 0 {
		window = 10 * time.Minute
	}
	return &Service{
		db:               database,
		issuer:           issuer,
		trustedDeviceTTL: ttl,
		stepUpWindow:     window,
		backupCodePepper: backupCodePepper,
	}, nil
}

// StartTOTPEnrollment provisions a TOTP secret and backup codes for the user.
func (s *Service) StartTOTPEnrollment(user *db.User) (*StartTOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if err := s.db.DeleteUnconfirmedMFAMethods(user.ID); err != nil {
		return nil, err
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: user.Email,
		Period:      totpPeriodSeconds,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	secret := key.Secret()
	uri := key.URL()

	method, err := s.db.CreateMFAMethod(user.ID, "totp", "Authenticator App", secret, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	backupCodes, hashes, err := s.generateBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.db.ReplaceBackupCodes(user.ID, hashes); err != nil {
		return nil, err
	}

	return &StartTOTPEnrollmentResult{
		MethodID:    method.ID,
		Secret:      secret,
		URI:         uri,
		BackupCodes: backupCodes,
	}, nil
}

// ConfirmTOTP finalizes enrollment by validating the provided code.
func (s *Service) ConfirmTOTP(user *db.User, methodID int64, code string) error {
	if user == nil {
		return fmt.Errorf("user required")
	}
	method, err := s.db.GetMFAMethodByID(methodID)
	if err != nil {
		return err
	}
	if method == nil || method.UserID != user.ID || method.Type != "totp" {
		return fmt.Errorf("invalid MFA method")
	}
	if err := s.validateTOTPCode(method.Secret, user.Email, code); err != nil {
		return err
	}

	now := time.Now()
	if err := s.db.ConfirmMFAMethod(method.ID, now); err != nil {
		return err
	}
	if err := s.db.SetUserMFAEnrolled(user.ID, true); err != nil {
		return err
	}
	return nil
}

// VerifyTOTP validates a TOTP or backup code during login or step-up.
func (s *Service) VerifyTOTP(user *db.User, code string) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	sanitized := strings.TrimSpace(code)
	if sanitized == "" {
		return nil, fmt.Errorf("code required")
	}

	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type != "totp" {
			continue
		}
		if err := s.validateTOTPCode(method.Secret, user.Email, sanitized); err == nil {
			now := time.Now()
			if method.ConfirmedAt == nil {
				if err := s.db.ConfirmMFAMethod(method.ID, now); err != nil {
					return nil, err
				}
			} else {
				if err := s.db.UpdateMFAMethodLastUsed(method.ID, now); err != nil {
					return nil, err
				}
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	// fallback to backup codes
	hash := s.hashBackupCode(sanitized)
	used, err := s.db.MarkBackupCodeUsed(user.ID, hash)
	if err != nil {
		return nil, err
	}
	if used {
		return &VerificationResult{MethodID: 0}, nil
	}

	return nil, fmt.Errorf("invalid code")
}

// GenerateBackupCodes replaces the user's backup codes and returns the plaintext set.
func (s *Service) GenerateBackupCodes(userID int64) ([]string, error) {
	codes, hashes, err := s.generateBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.db.ReplaceBackupCodes(userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

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

func (s *Service) generateBackupCodes() ([]string, []string, error) {
	codes := make([]string, 0, backupCodeCount)
	hashes := make([]string, 0, backupCodeCount)
	for i := 0; i < backupCodeCount; i++ {
		buf := make([]byte, backupCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		code := strings.ToUpper(hex.EncodeToString(buf))
		codes = append(codes, code)
		hashes = append(hashes, s.hashBackupCode(code))
	}
	return codes, hashes, nil
}

func (s *Service) hashBackupCode(code string) string {
	mac := hmac.New(sha256.New, s.backupCodePepper)
	mac.Write([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) validateTOTPCode(secret, email, code string) error {
	opts := totp.ValidateOpts{
		Period:    totpPeriodSeconds,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	ok, err := totp.ValidateCustom(code, secret, time.Now(), opts)
	if err != nil {
		return fmt.Errorf("invalid code: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid code")
	}
	return nil
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
