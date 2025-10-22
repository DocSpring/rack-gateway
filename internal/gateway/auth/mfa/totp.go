package mfa

import (
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
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
func (s *Service) VerifyTOTP(user *db.User, code string, ipAddress string, userAgent string, sessionID *int64) (*VerificationResult, error) {
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

	rateErr := s.enforceAttemptLimit(
		func(id int64, window int) (int, error) {
			return s.db.CountRecentTOTPAttempts(id, window)
		},
		user.ID,
		5,
		func() error {
			_ = s.db.LogTOTPAttempt(user.ID, nil, false, "rate_limited", ipAddress, userAgent, sessionID)
			return nil
		},
	)
	if rateErr != nil {
		return nil, rateErr
	}

	// Check if it's a Yubico OTP (44 characters)
	if len(sanitized) == 44 && s.yubiAuth != nil {
		result, err := s.VerifyYubiOTP(user, sanitized)
		if err == nil {
			return result, nil
		}
		// Fall through to try TOTP/backup codes
	}

	// Try TOTP verification
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type != "totp" {
			continue
		}

		// Validate code and get the time-step it's valid for
		timeStep, err := s.validateTOTPCodeWithTimeStep(method.Secret, sanitized)
		if err != nil {
			continue // Try next method
		}

		// Atomically consume the time-step (replay protection)
		consumed, err := s.db.ConsumeTOTPTimeStep(user.ID, timeStep, &method.ID, ipAddress, userAgent, sessionID)
		if err != nil {
			_ = s.db.LogTOTPAttempt(user.ID, &method.ID, false, "database_error", ipAddress, userAgent, sessionID)
			return nil, fmt.Errorf("verification failed")
		}
		if !consumed {
			// Time-step was already used (replay attack)
			_ = s.db.LogTOTPAttempt(user.ID, &method.ID, false, "replay_detected", ipAddress, userAgent, sessionID)
			if err := s.checkAndLockAccount(user.ID); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("verification failed")
		}

		// Success - log and update method
		_ = s.db.LogTOTPAttempt(user.ID, &method.ID, true, "", ipAddress, userAgent, sessionID)

		if err := s.touchMFAMethod(method); err != nil {
			return nil, err
		}
		return &VerificationResult{MethodID: method.ID}, nil
	}

	// Fallback to backup codes
	hash := s.hashBackupCode(sanitized)
	used, err := s.db.MarkBackupCodeUsed(user.ID, hash)
	if err != nil {
		return nil, err
	}
	if used {
		// Log successful backup code usage
		_ = s.db.LogTOTPAttempt(user.ID, nil, true, "backup_code", ipAddress, userAgent, sessionID)
		return &VerificationResult{MethodID: 0}, nil
	}

	// Log failed attempt (generic error, don't leak why it failed)
	_ = s.db.LogTOTPAttempt(user.ID, nil, false, "invalid_code", ipAddress, userAgent, sessionID)

	// Check if we should lock the account (5 failures in 5 minutes)
	if err := s.checkAndLockAccount(user.ID); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("verification failed")
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

func (s *Service) validateTOTPCode(secret, email, code string) error {
	_, err := s.validateTOTPCodeWithTimeStep(secret, code)
	return err
}
