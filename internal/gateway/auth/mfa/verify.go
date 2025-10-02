package mfa

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// VerifyCode attempts to verify any supported MFA code type.
// Supports TOTP (6 digits), Yubico OTP (44 chars), and backup codes (12 hex chars).
func (s *Service) VerifyCode(user *db.User, code string) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	sanitized := strings.TrimSpace(code)
	if sanitized == "" {
		return nil, fmt.Errorf("code required")
	}

	// Auto-detect code type by length and try verification
	switch len(sanitized) {
	case 44:
		// Yubico OTP (44 characters)
		if s.yubiAuth != nil {
			if result, err := s.VerifyYubiOTP(user, sanitized); err == nil {
				return result, nil
			}
		}
	case 6:
		// TOTP code (6 digits)
		if result, err := s.verifyTOTPOnly(user, sanitized); err == nil {
			return result, nil
		}
	case 12:
		// Backup code (12 hex characters)
		if result, err := s.verifyBackupCode(user, sanitized); err == nil {
			return result, nil
		}
	}

	// Fallback: try all methods
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}

	for _, method := range methods {
		switch method.Type {
		case "totp":
			if result, err := s.verifyTOTPWithMethod(user, method, sanitized); err == nil {
				return result, nil
			}
		case "yubiotp":
			if s.yubiAuth != nil {
				if result, err := s.verifyYubiOTPWithMethod(user, method, sanitized); err == nil {
					return result, nil
				}
			}
		}
	}

	// Final fallback: backup codes
	return s.verifyBackupCode(user, sanitized)
}

// verifyTOTPOnly tries TOTP verification without checking other methods
func (s *Service) verifyTOTPOnly(user *db.User, code string) (*VerificationResult, error) {
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type == "totp" {
			if result, err := s.verifyTOTPWithMethod(user, method, code); err == nil {
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("invalid TOTP code")
}

// verifyTOTPWithMethod validates a TOTP code against a specific method
func (s *Service) verifyTOTPWithMethod(user *db.User, method *db.MFAMethod, code string) (*VerificationResult, error) {
	if err := s.validateTOTPCode(method.Secret, user.Email, code); err != nil {
		return nil, err
	}
	return s.confirmMethodUsage(method)
}

// verifyYubiOTPWithMethod validates a Yubico OTP against a specific method
func (s *Service) verifyYubiOTPWithMethod(user *db.User, method *db.MFAMethod, otp string) (*VerificationResult, error) {
	publicID := otp[:12]
	if method.Secret != publicID {
		return nil, fmt.Errorf("yubikey public ID mismatch")
	}

	// Verify with Yubico servers
	_, ok, err := s.yubiAuth.Verify(otp)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Yubikey OTP: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid Yubikey OTP")
	}

	return s.confirmMethodUsage(method)
}

// verifyBackupCode attempts to verify and consume a backup code
func (s *Service) verifyBackupCode(user *db.User, code string) (*VerificationResult, error) {
	hash := s.hashBackupCode(code)
	used, err := s.db.MarkBackupCodeUsed(user.ID, hash)
	if err != nil {
		return nil, err
	}
	if !used {
		return nil, fmt.Errorf("invalid backup code")
	}
	return &VerificationResult{MethodID: 0}, nil
}

// confirmMethodUsage updates the method's confirmed/last_used timestamps
func (s *Service) confirmMethodUsage(method *db.MFAMethod) (*VerificationResult, error) {
	now := s.now()
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
