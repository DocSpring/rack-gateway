package mfa

import (
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// StartTOTPEnrollment provisions a TOTP secret and backup codes for the user.
func (s *Service) StartTOTPEnrollment(user *db.User) (*StartTOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if err := s.prepareEnrollment(user.ID); err != nil {
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

	method, err := s.db.CreateMFAMethod(user.ID, "totp", "Authenticator App", key.Secret(), nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	backupCodes, err := s.ensureBackupCodes(user.ID)
	if err != nil {
		return nil, err
	}
	// TOTP always generates backup codes on first enrollment
	if backupCodes == nil {
		codes, hashes, err := s.generateBackupCodes()
		if err != nil {
			return nil, err
		}
		if err := s.db.ReplaceBackupCodes(user.ID, hashes); err != nil {
			return nil, err
		}
		backupCodes = codes
	}

	return &StartTOTPEnrollmentResult{
		MethodID:    method.ID,
		Secret:      key.Secret(),
		URI:         key.URL(),
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

	return s.finalizeEnrollment(user.ID, method.ID)
}

// VerifyTOTP validates a TOTP or backup code during login or step-up.
// Also supports Yubico OTP if the code looks like a Yubikey OTP.
// Includes atomic time-step replay protection, rate limiting, and automatic account locking.
func (s *Service) VerifyTOTP(
	user *db.User,
	code string,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	sanitized := strings.TrimSpace(code)
	if sanitized == "" {
		return nil, fmt.Errorf("code required")
	}

	if err := s.ensureUserUnlocked(user.ID); err != nil {
		return nil, err
	}

	if err := s.enforceTOTPAttemptLimit(user.ID, ipAddress, userAgent, sessionID); err != nil {
		return nil, err
	}

	if result, handled := s.tryYubiOTP(user, sanitized); handled {
		return result, nil
	}

	if result, err := s.verifyTOTPMethods(user.ID, sanitized, ipAddress, userAgent, sessionID); err != nil ||
		result != nil {
		return result, err
	}

	if result, err := s.verifyBackupCodes(user.ID, sanitized, ipAddress, userAgent, sessionID); err != nil ||
		result != nil {
		return result, err
	}

	// Log failed attempt (generic error, don't leak why it failed)
	_ = s.db.LogTOTPAttempt(user.ID, nil, false, "invalid_code", ipAddress, userAgent, sessionID)

	// Check if we should lock the account (5 failures in 5 minutes)
	if err := s.checkAndLockAccount(user.ID); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("verification failed")
}

func (s *Service) enforceTOTPAttemptLimit(userID int64, ipAddress, userAgent string, sessionID *int64) error {
	return s.enforceAttemptLimit(
		func(id int64, window int) (int, error) {
			return s.db.CountRecentTOTPAttempts(id, window)
		},
		userID,
		5,
		func() error {
			return s.db.LogTOTPAttempt(userID, nil, false, "rate_limited", ipAddress, userAgent, sessionID)
		},
	)
}

func (s *Service) tryYubiOTP(user *db.User, code string) (*VerificationResult, bool) {
	if len(code) == 44 && s.yubiAuth != nil {
		if result, err := s.VerifyYubiOTP(user, code); err == nil {
			return result, true
		}
	}
	return nil, false
}

func (s *Service) verifyTOTPMethods(
	userID int64,
	code, ipAddress, userAgent string,
	sessionID *int64,
) (*VerificationResult, error) {
	methods, err := s.db.ListMFAMethods(userID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type != "totp" {
			continue
		}
		result, handled, err := s.verifySingleTOTPMethod(
			userID, method, code, ipAddress, userAgent, sessionID,
		)
		if handled {
			return result, err
		}
	}
	return nil, nil
}

func (s *Service) verifySingleTOTPMethod(
	userID int64,
	method *db.MFAMethod,
	code, ipAddress, userAgent string,
	sessionID *int64,
) (*VerificationResult, bool, error) {
	timeStep, err := s.validateTOTPCodeWithTimeStep(method.Secret, code)
	if err != nil {
		return nil, false, nil
	}

	consumed, err := s.db.ConsumeTOTPTimeStep(userID, timeStep, &method.ID, ipAddress, userAgent, sessionID)
	if err != nil {
		_ = s.db.LogTOTPAttempt(userID, &method.ID, false, "database_error", ipAddress, userAgent, sessionID)
		return nil, true, fmt.Errorf("verification failed")
	}
	if !consumed {
		_ = s.db.LogTOTPAttempt(userID, &method.ID, false, "replay_detected", ipAddress, userAgent, sessionID)
		if err := s.checkAndLockAccount(userID); err != nil {
			return nil, true, err
		}
		return nil, true, fmt.Errorf("verification failed")
	}

	_ = s.db.LogTOTPAttempt(userID, &method.ID, true, "", ipAddress, userAgent, sessionID)
	if err := s.touchMFAMethod(method); err != nil {
		return nil, true, err
	}
	return &VerificationResult{MethodID: method.ID}, true, nil
}

func (s *Service) verifyBackupCodes(
	userID int64,
	code, ipAddress, userAgent string,
	sessionID *int64,
) (*VerificationResult, error) {
	used, err := s.db.MarkBackupCodeUsed(userID, s.hashBackupCode(code))
	if err != nil {
		return nil, err
	}
	if used {
		_ = s.db.LogTOTPAttempt(userID, nil, true, "backup_code", ipAddress, userAgent, sessionID)
		return &VerificationResult{MethodID: 0}, nil
	}
	return nil, nil
}

// validateTOTPCodeWithTimeStep validates a TOTP code and returns the time-step it was valid for.
// Returns (timeStep, nil) on success, or (0, error) on failure.
func (s *Service) validateTOTPCodeWithTimeStep(secret, code string) (int64, error) {
	now := time.Now()
	currentStep := now.Unix() / totpPeriodSeconds

	// Try current step and adjacent steps (skew = 1)
	for skew := int64(-1); skew <= 1; skew++ {
		testStep := currentStep + skew
		testTime := time.Unix(testStep*totpPeriodSeconds, 0)

		expectedCode, err := totp.GenerateCodeCustom(secret, testTime, totp.ValidateOpts{
			Period:    totpPeriodSeconds,
			Skew:      0, // We're doing manual skew
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			continue
		}

		if expectedCode == strings.TrimSpace(code) {
			return testStep, nil
		}
	}

	return 0, fmt.Errorf("invalid code")
}

func (s *Service) validateTOTPCode(secret, _ /* email */, code string) error {
	_, err := s.validateTOTPCodeWithTimeStep(secret, code)
	return err
}
