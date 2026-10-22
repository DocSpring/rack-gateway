package mfa

import (
	"fmt"
	"time"

	"github.com/GeertJohan/yubigo"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
)

const (
	totpPeriodSeconds = 30
	backupCodeCount   = 10
	backupCodeBytes   = 6 // 12 hex chars
)

type yubiAuthVerifier interface {
	Verify(otp string) (*yubigo.YubiResponse, bool, error)
}

// Service orchestrates MFA enrollment, verification, and trusted devices.
type Service struct {
	db               *db.Database
	issuer           string
	settings         db.MFASettingsSource
	backupCodePepper []byte
	yubiAuth         yubiAuthVerifier
	webAuthn         *webauthn.WebAuthn
	emailSender      email.Sender
	timeFunc         func() time.Time
}

// IsWebAuthnConfigured returns true if WebAuthn is available.
func (s *Service) IsWebAuthnConfigured() bool {
	return s.webAuthn != nil
}

// now returns the current time (mockable for tests).
func (s *Service) now() time.Time {
	if s.timeFunc != nil {
		return s.timeFunc()
	}
	return time.Now()
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

// StartYubiOTPEnrollmentResult contains the Yubico OTP enrollment payload.
type StartYubiOTPEnrollmentResult struct {
	MethodID    int64
	BackupCodes []string
}

// StartWebAuthnEnrollmentResult contains the WebAuthn credential creation challenge.
type StartWebAuthnEnrollmentResult struct {
	MethodID         int64
	PublicKeyOptions *protocol.CredentialCreation
	BackupCodes      []string
}

// VerificationResult represents the outcome of an MFA verification.
type VerificationResult struct {
	MethodID        int64
	TrustedDeviceID *int64
}

// NewService creates a new MFA service instance.
func NewService(
	database *db.Database,
	issuer string,
	settings db.MFASettingsSource,
	backupCodePepper []byte,
	yubiClientID string,
	yubiSecretKey string,
	webAuthnRPID string,
	webAuthnOrigin string,
	emailSender email.Sender,
) (*Service, error) {
	if len(backupCodePepper) == 0 {
		return nil, fmt.Errorf("backup code pepper is required")
	}
	var yubiAuth yubiAuthVerifier
	if yubiClientID != "" && yubiSecretKey != "" {
		auth, err := yubigo.NewYubiAuth(yubiClientID, yubiSecretKey)
		if err != nil {
			return nil, fmt.Errorf("invalid Yubico OTP API credentials: %w", err)
		}
		yubiAuth = auth
	}

	var webAuthnClient *webauthn.WebAuthn
	if webAuthnRPID != "" && webAuthnOrigin != "" {
		wconfig := &webauthn.Config{
			RPDisplayName: issuer,
			RPID:          webAuthnRPID,
			RPOrigins:     []string{webAuthnOrigin},
		}
		client, err := webauthn.New(wconfig)
		if err != nil {
			return nil, fmt.Errorf("invalid WebAuthn configuration: %w", err)
		}
		webAuthnClient = client
	}

	return &Service{
		db:               database,
		issuer:           issuer,
		settings:         settings,
		backupCodePepper: backupCodePepper,
		yubiAuth:         yubiAuth,
		webAuthn:         webAuthnClient,
		emailSender:      emailSender,
	}, nil
}

// touchMFAMethod confirms a method if needed or updates its last-used timestamp.
func (s *Service) touchMFAMethod(method *db.MFAMethod) error {
	if method == nil {
		return fmt.Errorf("mfa method required")
	}
	now := s.now()
	if method.ConfirmedAt == nil {
		return s.db.ConfirmMFAMethod(method.ID, now)
	}
	return s.db.UpdateMFAMethodLastUsed(method.ID, now)
}
