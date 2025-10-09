package mfa

import (
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/GeertJohan/yubigo"
)

// mockYubiAuth implements a test double for Yubico OTP verification
type mockYubiAuth struct {
	verifyFunc func(otp string) (*yubigo.YubiResponse, bool, error)
}

func (m *mockYubiAuth) Verify(otp string) (*yubigo.YubiResponse, bool, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(otp)
	}
	return &yubigo.YubiResponse{}, true, nil
}

func TestYubiOTPEnrollment(t *testing.T) {
	t.Parallel()

	// Setup test database
	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper-for-mfa")
	service, err := NewService(database, "Test Gateway", 30*24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Inject mock Yubico auth
	mockAuth := &mockYubiAuth{
		verifyFunc: func(otp string) (*yubigo.YubiResponse, bool, error) {
			// Validate it's a valid Yubico OTP format
			if len(otp) != 44 {
				return nil, false, nil
			}
			return &yubigo.YubiResponse{}, true, nil
		},
	}
	service.yubiAuth = mockAuth

	// Create test user
	user, err := database.CreateUser("test@example.com", "Test User", []string{"admin"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	t.Run("successfully enrolls valid Yubikey OTP", func(t *testing.T) {
		// Simulate a valid Yubico OTP (12 char public ID + 32 char OTP)
		validOTP := "ccccccbcgujh" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"

		result, err := service.StartYubiOTPEnrollment(user, validOTP)
		if err != nil {
			t.Fatalf("expected successful enrollment, got error: %v", err)
		}

		if result.MethodID == 0 {
			t.Error("expected method ID to be set")
		}

		// Verify backup codes are generated on first enrollment
		if len(result.BackupCodes) == 0 {
			t.Error("expected backup codes on first enrollment")
		}

		// Verify the method was created in database
		methods, err := database.ListMFAMethods(user.ID)
		if err != nil {
			t.Fatalf("failed to list methods: %v", err)
		}
		if len(methods) != 1 {
			t.Fatalf("expected 1 method, got %d", len(methods))
		}
		if methods[0].Type != "yubiotp" {
			t.Errorf("expected type yubiotp, got %s", methods[0].Type)
		}
		if methods[0].Secret != "ccccccbcgujh" {
			t.Errorf("expected public ID ccccccbcgujh, got %s", methods[0].Secret)
		}
		if methods[0].ConfirmedAt == nil {
			t.Error("expected method to be auto-confirmed")
		}

		// Verify user is marked as MFA enrolled
		updatedUser, err := database.GetUser(user.Email)
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if !updatedUser.MFAEnrolled {
			t.Error("expected user to be MFA enrolled")
		}
	})

	t.Run("rejects invalid OTP format", func(t *testing.T) {
		_, err := service.StartYubiOTPEnrollment(user, "short")
		if err == nil {
			t.Error("expected error for invalid OTP format")
		}
	})

	t.Run("rejects duplicate Yubikey", func(t *testing.T) {
		// Try to enroll the same Yubikey again
		validOTP := "ccccccbcgujh" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
		_, err := service.StartYubiOTPEnrollment(user, validOTP)
		if err == nil {
			t.Error("expected error for duplicate Yubikey")
		}
		if err != nil && err.Error() != "this Yubikey is already registered" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("rejects when Yubico OTP not configured", func(t *testing.T) {
		serviceWithoutYubi, _ := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
		_, err := serviceWithoutYubi.StartYubiOTPEnrollment(user, "ccccccbcgujhnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn")
		if err == nil {
			t.Error("expected error when Yubico OTP not configured")
		}
	})
}

func TestYubiOTPVerification(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper")
	service, _ := NewService(database, "Test Gateway", 30*24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)

	mockAuth := &mockYubiAuth{
		verifyFunc: func(otp string) (*yubigo.YubiResponse, bool, error) {
			if len(otp) != 44 {
				return nil, false, nil
			}
			// Simulate successful verification for this specific public ID
			if otp[:12] == "ccccccbcgujh" {
				return &yubigo.YubiResponse{}, true, nil
			}
			return nil, false, nil
		},
	}
	service.yubiAuth = mockAuth

	user, _ := database.CreateUser("yubitest@example.com", "Yubi Test", []string{"ops"})

	// Enroll a Yubikey
	enrollOTP := "ccccccbcgujh" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
	result, err := service.StartYubiOTPEnrollment(user, enrollOTP)
	if err != nil {
		t.Fatalf("failed to enroll: %v", err)
	}
	methodID := result.MethodID

	t.Run("successfully verifies correct OTP", func(t *testing.T) {
		verifyOTP := "ccccccbcgujh" + "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm" // Different OTP, same public ID
		verifyResult, err := service.VerifyYubiOTP(user, verifyOTP)
		if err != nil {
			t.Fatalf("expected successful verification, got error: %v", err)
		}
		if verifyResult.MethodID != methodID {
			t.Errorf("expected method ID %d, got %d", methodID, verifyResult.MethodID)
		}

		// Verify last_used_at was updated
		method, _ := database.GetMFAMethodByID(methodID)
		if method.LastUsedAt == nil {
			t.Error("expected last_used_at to be set")
		}
	})

	t.Run("rejects OTP from unregistered Yubikey", func(t *testing.T) {
		wrongOTP := "dddddddddddd" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
		_, err := service.VerifyYubiOTP(user, wrongOTP)
		if err == nil {
			t.Error("expected error for unregistered Yubikey")
		}
	})

	t.Run("rejects invalid OTP format", func(t *testing.T) {
		_, err := service.VerifyYubiOTP(user, "short")
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})
}
