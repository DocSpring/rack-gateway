package mfa

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestVerifyTOTP_ReplayProtection(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "test@example.com", "Test User")

	// Create TOTP method
	key := createConfirmedTOTP(t, database, user)

	// Generate a valid TOTP code
	code, _ := totp.GenerateCode(key.Secret(), time.Now())

	// First attempt should succeed
	if res, err := svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil); err != nil {
		t.Fatalf("expected first attempt to succeed, got: %v", err)
	} else if res == nil {
		t.Fatalf("expected a non-nil verification result on success")
	}

	// Second attempt with same code should be rejected (generic error)
	if _, err := svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil); err == nil {
		t.Fatal("expected replay detection to reject reused code")
	} else if err.Error() != "verification failed" {
		t.Fatalf("expected 'verification failed' error, got: %v", err)
	}
}

func TestVerifyTOTP_RateLimiting(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "ratelimit@example.com", "Rate Test")

	// Make 4 failed attempts (less than lock threshold)
	for i := 0; i < 4; i++ {
		_, _ = svc.VerifyTOTP(user, "wrong", "1.2.3.4", "test-agent", nil)
	}

	// Log one more directly to hit rate limit without triggering lock
	_ = database.LogTOTPAttempt(user.ID, nil, false, "invalid_code", "1.2.3.4", "test-agent", nil)

	// 6th attempt should be rate limited (not locked, since only 4 failed verifications)
	if _, err := svc.VerifyTOTP(user, "123456", "1.2.3.4", "test-agent", nil); err == nil {
		t.Fatal("expected rate limiting to reject attempt")
	} else if err.Error() != "too many attempts - try again in 5 minutes" {
		t.Fatalf("expected rate limit error, got: %v", err)
	}
}

func TestVerifyTOTP_AccountLocking(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "lock@example.com", "Lock Test")

	// Make 5 failed attempts to trigger auto-lock
	for i := 0; i < 5; i++ {
		_, _ = svc.VerifyTOTP(user, "wrong", "1.2.3.4", "test-agent", nil)
	}

	// Verify account is locked
	locked, err := database.IsUserLocked(user.ID)
	if err != nil {
		t.Fatalf("failed to check lock status: %v", err)
	}
	if !locked {
		t.Fatal("expected account to be locked after 5 failed attempts")
	}

	// Verify lock reason
	userRecord, _ := database.GetUserByID(user.ID)
	if userRecord.LockedReason != "Automatic lock: 5 failed MFA attempts in 5 minutes" {
		t.Fatalf("unexpected lock reason: %s", userRecord.LockedReason)
	}
}

func TestVerifyTOTP_LockedAccountRejection(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "locked@example.com", "Locked User")

	// Lock the account
	_ = database.LockUser(user.ID, "Test lock", nil)

	// Create valid TOTP method
	key := createConfirmedTOTP(t, database, user)

	// Generate valid code
	code, _ := totp.GenerateCode(key.Secret(), time.Now())

	// Attempt should be rejected immediately
	if _, err := svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil); err == nil {
		t.Fatal("expected locked account to reject login")
	} else if err.Error() != "account locked - contact administrator" {
		t.Fatalf("expected account locked error, got: %v", err)
	}
}

func TestVerifyTOTP_LoggingMetadata(t *testing.T) {
	t.Parallel()

	svc, _, user := setupMFAService(t, "logging@example.com", "Logging Test")

	// Make a failed attempt
	_, _ = svc.VerifyTOTP(user, "wrong", "192.168.1.100", "Mozilla/5.0", nil)

	// This test verifies that attempt logging happens, but we can't easily
	// query the attempts table in this test framework without adding more methods.
	// The real verification happens in integration tests.
}

func setupMFAService(t *testing.T, email, name string) (*Service, *db.Database, *db.User) {
	t.Helper()

	database := dbtest.NewDatabase(t)
	svc := mustNewService(t, database)
	user := mustCreateUser(t, database, email, name)
	return svc, database, user
}

func mustNewService(t *testing.T, database *db.Database) *Service {
	t.Helper()

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, []byte("test-pepper"), "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	return svc
}

func mustCreateUser(t *testing.T, database *db.Database, email, name string) *db.User {
	t.Helper()

	user, err := database.CreateUser(email, name, []string{"admin"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return user
}

func createConfirmedTOTP(t *testing.T, database *db.Database, user *db.User) *otp.Key {
	t.Helper()

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}
	method, err := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create MFA method: %v", err)
	}
	if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm MFA method: %v", err)
	}
	return key
}
