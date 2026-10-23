package mfa

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// yubiOTPPattern is a Yubico OTP: a public ID of 1 to 16 modhex characters, then 32 encrypted ones. An OTP from a key
// without a public ID couldn't be told apart from another key's.
var yubiOTPPattern = regexp.MustCompile(`^[cbdefghijklnrtuv]{33,48}$`)

// yubiOTPPublicID returns the public ID of a Yubico OTP, which identifies the key, or false for anything else.
// (yubigo.ParseOTP would take some modhex OTPs for Dvorak keystrokes and translate them.)
func yubiOTPPublicID(otp string) (string, bool) {
	otp = strings.ToLower(otp)
	if !yubiOTPPattern.MatchString(otp) {
		return "", false
	}
	return otp[:len(otp)-32], true
}

// StartYubiOTPEnrollment provisions a Yubico OTP method for the user, labeled label ("Yubikey" when blank).
// Auto-confirms since we validate the OTP during enrollment.
func (s *Service) StartYubiOTPEnrollment(
	user *db.User, yubiOTP, label string,
) (*StartYubiOTPEnrollmentResult, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.yubiAuth == nil {
		return nil, fmt.Errorf("yubico OTP not configured")
	}

	yubiOTP = strings.TrimSpace(yubiOTP)
	publicID, ok := yubiOTPPublicID(yubiOTP)
	if !ok {
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

	if err := s.prepareEnrollment(user.ID); err != nil {
		return nil, err
	}

	if err := s.checkDuplicateYubikey(user.ID, publicID); err != nil {
		return nil, err
	}

	label = strings.TrimSpace(label)
	if label == "" {
		label = "Yubikey"
	}
	method, err := s.db.CreateMFAMethod(user.ID, "yubiotp", label, publicID, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// Store the backup codes before the key is enrolled: when they can't be stored, nothing is enrolled.
	backupCodes, err := s.backupCodesForEnrollment(user)
	if err != nil {
		return nil, err
	}

	// Auto-confirm since we validated the OTP
	if err := s.finalizeEnrollment(user.ID, method.ID, nil); err != nil {
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
	publicID, ok := yubiOTPPublicID(otp)
	if !ok {
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

	// Find matching method
	methods, err := s.db.ListMFAMethods(user.ID)
	if err != nil {
		return nil, err
	}
	for _, method := range methodsWithSecret(methods, "yubiotp") {
		if method.Secret == publicID {
			if err := s.touchMFAMethod(method); err != nil {
				return nil, err
			}
			return &VerificationResult{MethodID: method.ID}, nil
		}
	}

	return nil, fmt.Errorf("yubikey not registered")
}
