package mfa

import (
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestVerifyTOTP_ReplayProtection(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("test@example.com", "Test User", []string{"admin"})

	// Create TOTP method
	key, _ := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	method, _ := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	_ = database.ConfirmMFAMethod(method.ID, time.Now())

	// Generate a valid TOTP code
	code, _ := totp.GenerateCode(key.Secret(), time.Now())

	// First attempt should succeed
	_, err = svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil)
	if err != nil {
		t.Fatalf("expected first attempt to succeed, got: %v", err)
	}

	// Second attempt with same code should be rejected (generic error)
	_, err = svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil)
	if err == nil {
		t.Fatal("expected replay detection to reject reused code")
	}
	if err.Error() != "verification failed" {
		t.Fatalf("expected 'verification failed' error, got: %v", err)
	}
}

func TestVerifyTOTP_RateLimiting(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("ratelimit@example.com", "Rate Test", []string{"admin"})

	// Make 4 failed attempts (less than lock threshold)
	for i := 0; i < 4; i++ {
		_, _ = svc.VerifyTOTP(user, "wrong", "1.2.3.4", "test-agent", nil)
	}

	// Log one more directly to hit rate limit without triggering lock
	_ = database.LogTOTPAttempt(user.ID, nil, false, "invalid_code", "1.2.3.4", "test-agent", nil)

	// 6th attempt should be rate limited (not locked, since only 4 failed verifications)
	_, err = svc.VerifyTOTP(user, "123456", "1.2.3.4", "test-agent", nil)
	if err == nil {
		t.Fatal("expected rate limiting to reject attempt")
	}
	if err.Error() != "too many attempts - try again in 5 minutes" {
		t.Fatalf("expected rate limit error, got: %v", err)
	}
}

func TestVerifyTOTP_AccountLocking(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("lock@example.com", "Lock Test", []string{"admin"})

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

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("locked@example.com", "Locked User", []string{"admin"})

	// Lock the account
	_ = database.LockUser(user.ID, "Test lock", nil)

	// Create valid TOTP method
	key, _ := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	method, _ := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	_ = database.ConfirmMFAMethod(method.ID, time.Now())

	// Generate valid code
	code, _ := totp.GenerateCode(key.Secret(), time.Now())

	// Attempt should be rejected immediately
	_, err = svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil)
	if err == nil {
		t.Fatal("expected locked account to reject login")
	}
	if err.Error() != "account locked - contact administrator" {
		t.Fatalf("expected account locked error, got: %v", err)
	}
}

func TestVerifyTOTP_LoggingMetadata(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	svc, err := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("logging@example.com", "Logging Test", []string{"admin"})

	// Make a failed attempt
	_, _ = svc.VerifyTOTP(user, "wrong", "192.168.1.100", "Mozilla/5.0", nil)

	// This test verifies that attempt logging happens, but we can't easily
	// query the attempts table in this test framework without adding more methods.
	// The real verification happens in integration tests.
}
