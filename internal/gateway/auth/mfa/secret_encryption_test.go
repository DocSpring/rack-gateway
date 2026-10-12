package mfa

import (
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
)

// codeForEmptySecret returns the current code for an empty secret. pquerna/otp accepts an empty secret: it
// decodes to an empty HMAC key, so anyone can compute these codes.
func codeForEmptySecret(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom("", time.Now(), totp.ValidateOpts{
		Period:    totpPeriodSeconds,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("expected a code for an empty secret: %v", err)
	}
	return code
}

func TestValidateTOTPRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	code := codeForEmptySecret(t)
	svc := &Service{}
	for _, secret := range []string{"", "   ", "===="} {
		if _, err := svc.validateTOTPCodeWithTimeStep(secret, code); err == nil {
			t.Errorf("secret %q verified a code computed from an empty key", secret)
		}
	}
}

func TestVerifyTOTPRejectsEmptyStoredSecret(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "empty-secret@example.com", "Empty Secret")
	method, err := database.CreateMFAMethod(user.ID, "totp", "Authenticator", "", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create MFA method: %v", err)
	}
	if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm MFA method: %v", err)
	}

	if result, err := svc.VerifyTOTP(user, codeForEmptySecret(t), "127.0.0.1", "test", nil); err == nil {
		t.Fatalf("a code from an empty key verified (method %d)", result.MethodID)
	}
}

func TestConfirmTOTPRejectsUndecryptableSecret(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "confirm-planted@example.com", "Confirm Planted")
	start, err := svc.StartTOTPEnrollment(user)
	if err != nil {
		t.Fatalf("failed to start enrollment: %v", err)
	}
	plantSecret(t, database, start.MethodID, start.Secret)

	code, err := totp.GenerateCode(start.Secret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate code: %v", err)
	}
	if err := svc.ConfirmTOTP(user, start.MethodID, code); err == nil {
		t.Fatal("a method with a plaintext secret in the database was confirmed")
	}
}

// plantSecret writes a plaintext secret straight into the database, as someone with write access could.
func plantSecret(t *testing.T, database *db.Database, methodID int64, secret string) {
	t.Helper()
	if _, err := database.DB().Exec(`UPDATE mfa_methods SET secret = $1 WHERE id = $2`, secret, methodID); err != nil {
		t.Fatalf("failed to plant secret: %v", err)
	}
}

func TestUndecryptableTOTPMethodDoesNotBlockOtherMethods(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	svc, err := NewService(
		database, "Test", nil, []byte("test-pepper"),
		"", "", "localhost", "http://localhost", nil,
	)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	user := mustCreateUser(t, database, "undecryptable@example.com", "Undecryptable")

	credential, err := webauthntest.GenerateMockCredential()
	if err != nil {
		t.Fatalf("failed to generate mock credential: %v", err)
	}
	webauthnMethod, err := database.CreateMFAMethod(
		user.ID, "webauthn", "Key", "", credential.ID, credential.PublicKey, nil, nil,
	)
	if err != nil {
		t.Fatalf("failed to create WebAuthn method: %v", err)
	}
	if err := database.ConfirmMFAMethod(webauthnMethod.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm WebAuthn method: %v", err)
	}

	key := createConfirmedTOTP(t, database, user)
	methods, err := database.ListMFAMethods(user.ID)
	if err != nil {
		t.Fatalf("failed to list methods: %v", err)
	}
	for _, method := range methods {
		if method.Type == "totp" {
			plantSecret(t, database, method.ID, key.Secret())
		}
	}

	assertMethodsListed(t, database, user.ID)
	assertPlantedTOTPRejected(t, svc, user, key.Secret())
	assertWebAuthnStillWorks(t, svc, user, credential, webauthnMethod.ID)
	assertBackupCodeStillWorks(t, svc, user)
}

func assertMethodsListed(t *testing.T, database *db.Database, userID int64) {
	t.Helper()
	methods, err := database.ListMFAMethods(userID)
	if err != nil {
		t.Fatalf("listing methods failed: %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("expected both methods to be listed, got %d", len(methods))
	}
	for _, method := range methods {
		if method.Type == "totp" && (method.SecretErr == nil || method.Secret != "") {
			t.Fatalf("expected the TOTP method to be unusable, err=%v", method.SecretErr)
		}
	}
}

func assertPlantedTOTPRejected(t *testing.T, svc *Service, user *db.User, planted string) {
	t.Helper()
	code, err := totp.GenerateCode(planted, time.Now())
	if err != nil {
		t.Fatalf("failed to generate code: %v", err)
	}
	if _, err := svc.VerifyTOTP(user, code, "127.0.0.1", "test", nil); err == nil {
		t.Fatal("a code from a planted plaintext seed verified")
	}
}

func assertWebAuthnStillWorks(
	t *testing.T,
	svc *Service,
	user *db.User,
	credential *webauthntest.MockCredential,
	methodID int64,
) {
	t.Helper()
	options, challengeID, err := svc.StartWebAuthnAssertion(user, nil)
	if err != nil {
		t.Fatalf("failed to start assertion: %v", err)
	}
	assertion, err := credential.GenerateAssertion(options, "http://localhost")
	if err != nil {
		t.Fatalf("failed to generate assertion: %v", err)
	}
	result, err := svc.VerifyWebAuthnAssertion(
		user, []byte(challengeID), []byte(assertion), "127.0.0.1", "test", nil,
	)
	if err != nil {
		t.Fatalf("WebAuthn failed next to an undecryptable TOTP method: %v", err)
	}
	if result.MethodID != methodID {
		t.Fatalf("expected method %d, got %d", methodID, result.MethodID)
	}
}

func assertBackupCodeStillWorks(t *testing.T, svc *Service, user *db.User) {
	t.Helper()
	codes, err := svc.GenerateBackupCodes(user.ID)
	if err != nil || len(codes) == 0 {
		t.Fatalf("failed to generate backup codes: %v", err)
	}
	if _, err := svc.VerifyTOTP(user, codes[0], "127.0.0.1", "test", nil); err != nil {
		t.Fatalf("backup code failed next to an undecryptable TOTP method: %v", err)
	}
}

// A user whose authenticator app is unusable keeps entering codes from it. Those attempts must not lock the
// account, which would also block the backup code that gets them back in.
func TestUnusableTOTPAttemptsDoNotLockTheAccount(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	svc, err := NewService(
		database, "Test", nil, []byte("test-pepper"),
		"", "", "localhost", "http://localhost", nil,
	)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	user := mustCreateUser(t, database, "unusable@example.com", "Unusable")
	key := createConfirmedTOTP(t, database, user)
	methods, err := database.ListMFAMethods(user.ID)
	if err != nil {
		t.Fatalf("failed to list methods: %v", err)
	}
	for _, method := range methods {
		plantSecret(t, database, method.ID, key.Secret()) // not encrypted, so unusable
	}
	backupCodes, err := svc.GenerateBackupCodes(user.ID)
	if err != nil {
		t.Fatalf("failed to generate backup codes: %v", err)
	}

	for range 6 {
		code, err := totp.GenerateCode(key.Secret(), time.Now())
		if err != nil {
			t.Fatalf("failed to generate code: %v", err)
		}
		if _, err := svc.VerifyTOTP(user, code, "127.0.0.1", "test", nil); !errors.Is(err, ErrTOTPUnusable) {
			t.Fatalf("VerifyTOTP err = %v, want ErrTOTPUnusable", err)
		}
	}
	if locked, err := database.IsUserLocked(user.ID); err != nil || locked {
		t.Fatalf("IsUserLocked = %v, %v; want unlocked", locked, err)
	}
	if _, err := svc.VerifyTOTP(user, backupCodes[0], "127.0.0.1", "test", nil); err != nil {
		t.Fatalf("backup code: %v", err)
	}
}
