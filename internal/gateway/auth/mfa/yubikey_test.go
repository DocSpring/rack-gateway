package mfa

import (
	"strings"
	"testing"

	"github.com/GeertJohan/yubigo"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
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

func TestYubiOTPEnrollment_Success(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper-for-mfa")
	service, err := NewService(database, "Test Gateway", nil, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	mockAuth := &mockYubiAuth{
		verifyFunc: func(otp string) (*yubigo.YubiResponse, bool, error) {
			if len(otp) != 44 {
				return nil, false, nil
			}
			return &yubigo.YubiResponse{}, true, nil
		},
	}
	service.yubiAuth = mockAuth

	user, err := database.CreateUser("test@example.com", "Test User", []string{"admin"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	validOTP := "ccccccbcgujh" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
	result, err := service.StartYubiOTPEnrollment(user, validOTP, "")
	if err != nil {
		t.Fatalf("expected successful enrollment, got error: %v", err)
	}

	if result.MethodID == 0 {
		t.Error("expected method ID to be set")
	}

	if len(result.BackupCodes) == 0 {
		t.Error("expected backup codes on first enrollment")
	}

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

	updatedUser, err := database.GetUser(user.Email)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if !updatedUser.MFAEnrolled {
		t.Error("expected user to be MFA enrolled")
	}
}

func TestYubiOTPEnrollment_InvalidFormat(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper-for-mfa")
	service, err := NewService(database, "Test Gateway", nil, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	mockAuth := &mockYubiAuth{
		verifyFunc: func(otp string) (*yubigo.YubiResponse, bool, error) {
			if len(otp) != 44 {
				return nil, false, nil
			}
			return &yubigo.YubiResponse{}, true, nil
		},
	}
	service.yubiAuth = mockAuth

	user, _ := database.CreateUser("test2@example.com", "Test User", []string{"admin"})

	_, err = service.StartYubiOTPEnrollment(user, "short", "")
	if err == nil {
		t.Error("expected error for invalid OTP format")
	}
}

func TestYubiOTPEnrollment_Duplicate(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper-for-mfa")
	service, err := NewService(database, "Test Gateway", nil, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	mockAuth := &mockYubiAuth{
		verifyFunc: func(otp string) (*yubigo.YubiResponse, bool, error) {
			if len(otp) != 44 {
				return nil, false, nil
			}
			return &yubigo.YubiResponse{}, true, nil
		},
	}
	service.yubiAuth = mockAuth

	user, _ := database.CreateUser("test3@example.com", "Test User", []string{"admin"})

	validOTP := "ccccccbcgujh" + "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
	_, err = service.StartYubiOTPEnrollment(user, validOTP, "")
	if err != nil {
		t.Fatalf("first enrollment failed: %v", err)
	}

	// Try to enroll the same Yubikey again
	_, err = service.StartYubiOTPEnrollment(user, validOTP, "")
	if err == nil {
		t.Error("expected error for duplicate Yubikey")
	}
	if err != nil && err.Error() != "this Yubikey is already registered" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestYubiOTPEnrollment_NotConfigured(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper-for-mfa")
	serviceWithoutYubi, _ := NewService(database, "Test", nil, pepper, "", "", "", "", nil)

	user, _ := database.CreateUser("test4@example.com", "Test User", []string{"admin"})

	_, err := serviceWithoutYubi.StartYubiOTPEnrollment(user, "ccccccbcgujhnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn", "")
	if err == nil {
		t.Error("expected error when Yubico OTP not configured")
	}
}

func TestYubiOTPVerification(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper")
	service, _ := NewService(database, "Test Gateway", nil, pepper, "", "", "", "", nil)

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
	result, err := service.StartYubiOTPEnrollment(user, enrollOTP, "")
	if err != nil {
		t.Fatalf("failed to enroll: %v", err)
	}
	methodID := result.MethodID

	t.Run("successfully verifies correct OTP", func(t *testing.T) {
		verifyOTP := "ccccccbcgujh" + "llllllllllllllllllllllllllllllll" // Different OTP, same public ID
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

// A YubiKey's public ID is everything before the OTP's 32-character encrypted part, 1 to 16 characters long. A key
// with a public ID other than 12 characters enrolls, keeps its label, and verifies through VerifyTOTP like any other.
func TestYubiOTPPublicIDLengths(t *testing.T) {
	t.Parallel()

	service, database, user := setupMFAService(t, "yubi-lengths@example.com", "Yubi Lengths")
	service.yubiAuth = &mockYubiAuth{}

	encrypted := strings.Repeat("n", 32)
	if _, err := service.StartYubiOTPEnrollment(user, encrypted, ""); err == nil {
		t.Fatal("expected an OTP without a public ID to be refused")
	}
	if _, err := service.StartYubiOTPEnrollment(user, strings.Repeat("c", 17)+encrypted, ""); err == nil {
		t.Fatal("expected an OTP with a 17-character public ID to be refused")
	}

	publicID := "vvccccbcgujhnrtu"
	result, err := service.StartYubiOTPEnrollment(user, publicID+encrypted, "  Desk key  ")
	if err != nil {
		t.Fatalf("expected a 16-character public ID to enroll: %v", err)
	}
	method, err := database.GetMFAMethodByID(result.MethodID)
	if err != nil {
		t.Fatalf("failed to load the method: %v", err)
	}
	if method.Secret != publicID || method.Label != "Desk key" {
		t.Fatalf("expected public ID %q labeled %q, got %q labeled %q", publicID, "Desk key", method.Secret,
			method.Label)
	}

	enrolled, err := database.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("failed to reload the user: %v", err)
	}
	verified, err := service.VerifyTOTP(enrolled, publicID+strings.Repeat("l", 32), "127.0.0.1", "test", nil)
	if err != nil || verified.MethodID != result.MethodID {
		t.Fatalf("expected the key's next OTP to verify, got %+v, %v", verified, err)
	}
}
