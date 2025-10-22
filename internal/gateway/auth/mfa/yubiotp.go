package mfa

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// StartYubiOTPEnrollment provisions a Yubico OTP method for the user.
// Auto-confirms since we validate the OTP during enrollment.
func (s *Service) StartYubiOTPEnrollment(user *db.User, yubiOTP string) (*StartYubiOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.yubiAuth == nil {
		return nil, fmt.Errorf("yubico OTP not configured")
	}

	yubiOTP = strings.TrimSpace(yubiOTP)
	if len(yubiOTP) < 32 {
		return nil, fmt.Errorf("invalid Yubikey OTP format")
	}

	// Verify the OTP with Yubico servers
	_, ok, err := s.yubiAuth.Verify(yubiOTP)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Yubikey OTP: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid Yubikey OTP")
	}

	// Extract the public ID (first 12 characters)
	publicID := yubiOTP[:12]

	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, err
	}

	if err := s.checkDuplicateYubikey(user.ID, publicID); err != nil {
		return nil, err
	}

	method, err := s.db.CreateMFAMethod(user.ID, "yubiotp", "Yubikey", publicID, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// Auto-confirm since we validated the OTP
	if err := s.finalizeEnrollment(user.ID, method.ID); err != nil {
		return nil, err
	}

	backupCodes, err := s.ensureBackupCodes(user.ID)
	if err != nil {
		return nil, err
	}

	return &StartYubiOTPEnrollmentResult{
		MethodID:    method.ID,
		BackupCodes: backupCodes,
	}, nil
}

// VerifyYubiOTP validates a Yubico OTP during login or step-up.
func (s *Service) VerifyYubiOTP(user *db.User, otp string) (*VerificationResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.yubiAuth == nil {
		return nil, fmt.Errorf("yubico OTP not configured")
	}

	otp = strings.TrimSpace(otp)
	if len(otp) < 32 {
		return nil, fmt.Errorf("invalid Yubikey OTP format")
	}

	// Verify with Yubico servers
	_, ok, err := s.yubiAuth.Verify(otp)
	if err != nil {
		return nil, fmt.Errorf("failed to verify Yubikey OTP: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid Yubikey OTP")
	}

	// Extract public ID
	publicID := otp[:12]

	// Find matching method
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methods {
		if method.Type == "yubiotp" && method.Secret == publicID {
			if err := s.touchMFAMethod(method); err != nil {
				return nil, err
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	return nil, fmt.Errorf("yubikey not registered")
}
