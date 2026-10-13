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
		auth, _ := yubigo.NewYubiAuth(yubiClientID, yubiSecretKey)
		yubiAuth = auth
	}

	var webAuthnClient *webauthn.WebAuthn
	if webAuthnRPID != "" && webAuthnOrigin != "" {
		wconfig := &webauthn.Config{
			RPDisplayName: issuer,
			RPID:          webAuthnRPID,
			RPOrigins:     []string{webAuthnOrigin},
		}
		webAuthnClient, _ = webauthn.New(wconfig)
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

func (s *Service) ensureUserUnlocked(userID int64) error {
	locked, err := s.db.IsUserLocked(userID)
	if err != nil {
		return fmt.Errorf("failed to check account lock status: %w", err)
	}
	if locked {
		return fmt.Errorf("account locked - contact administrator")
	}
	return nil
}

func (_ *Service) enforceAttemptLimit(
	counter func(int64, int) (int, error),
	userID int64,
	window int,
	onLimit func() error,
) error {
	attempts, err := counter(userID, window)
	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}
	if attempts >= window {
		if onLimit != nil {
			if err := onLimit(); err != nil {
				return err
			}
		}
		return fmt.Errorf("too many attempts - try again in 5 minutes")
	}
	return nil
}
