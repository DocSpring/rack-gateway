package mfa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
)

func TestMockCredentialGeneratesValidAssertion(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)
	pepper := []byte("test-pepper")

	service, err := NewService(database, "Test Gateway", 24*time.Hour, 10*time.Minute, pepper, "", "", "localhost", "http://localhost", nil)
	if err != nil {
		t.Fatalf("failed to create MFA service: %v", err)
	}

	user, err := database.CreateUser("mock-credential@example.com", "Mock Credential", []string{"admin"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	credential, err := webauthntest.GenerateMockCredential()
	if err != nil {
		t.Fatalf("failed to generate mock credential: %v", err)
	}

	method, err := database.CreateMFAMethod(user.ID, "webauthn", "Test Credential", "", credential.ID, credential.PublicKey, nil, nil)
	if err != nil {
		t.Fatalf("failed to create MFA method: %v", err)
	}
	if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm MFA method: %v", err)
	}

	_, sessionData, err := service.StartWebAuthnAssertion(user)
	if err != nil {
		t.Fatalf("failed to start assertion: %v", err)
	}

	assertionJSON, err := credential.GenerateAssertionForSession(sessionData, "http://localhost")
	if err != nil {
		t.Fatalf("failed to generate assertion: %v", err)
	}

	result, err := service.VerifyWebAuthnAssertion(user, sessionData, []byte(assertionJSON), "127.0.0.1", "test-agent", nil)
	if err != nil {
		t.Fatalf("failed to verify assertion: %v", err)
	}

	if result.MethodID != method.ID {
		t.Fatalf("expected method ID %d, got %d", method.ID, result.MethodID)
	}
}

func TestWebAuthnEnrollment(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper")

	// Create service with WebAuthn configured
	service, err := NewService(database, "Test Gateway", 24*time.Hour, 10*time.Minute, pepper, "", "", "example.com", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	user, _ := database.CreateUser("webauthn@example.com", "WebAuthn Test", []string{"admin"})

	t.Run("successfully starts WebAuthn enrollment", func(t *testing.T) {
		result, sessionData, err := service.StartWebAuthnEnrollment(user)
		if err != nil {
			t.Fatalf("expected successful enrollment start, got error: %v", err)
		}

		if result.MethodID == 0 {
			t.Error("expected method ID to be set")
		}

		if result.PublicKeyOptions == nil {
			t.Error("expected public key options to be returned")
		}

		if sessionData == "" {
			t.Error("expected session data to be returned")
		}

		// Verify session data is valid JSON
		var session webauthn.SessionData
		if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
			t.Errorf("session data should be valid JSON: %v", err)
		}

		// Verify backup codes are generated on first enrollment
		if len(result.BackupCodes) == 0 {
			t.Error("expected backup codes on first enrollment")
		}
	})

	t.Run("fails when WebAuthn not configured", func(t *testing.T) {
		serviceWithoutWebAuthn, _ := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
		_, _, err := serviceWithoutWebAuthn.StartWebAuthnEnrollment(user)
		if err == nil {
			t.Error("expected error when WebAuthn not configured")
		}
	})

	t.Run("cleans up unconfirmed methods before enrollment", func(t *testing.T) {
		// Start enrollment twice to ensure cleanup works
		result1, _, _ := service.StartWebAuthnEnrollment(user)
		result2, _, _ := service.StartWebAuthnEnrollment(user)

		// Second enrollment should have cleaned up first
		if result1.MethodID == result2.MethodID {
			t.Error("expected new method ID after cleanup")
		}

		// Verify only pending methods exist
		methods, _ := database.ListMFAMethods(user.ID)
		unconfirmedCount := 0
		for _, m := range methods {
			if m.ConfirmedAt == nil {
				unconfirmedCount++
			}
		}
		if unconfirmedCount > 1 {
			t.Errorf("expected at most 1 unconfirmed method, got %d", unconfirmedCount)
		}
	})
}

func TestWebAuthnUserInterface(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	user, _ := database.CreateUser("wauser@example.com", "WebAuthn User", []string{"ops"})

	// Create some WebAuthn methods
	credID1 := []byte("credential-id-1")
	pubKey1 := []byte("public-key-1")
	method1, _ := database.CreateMFAMethod(user.ID, "webauthn", "Key 1", "", credID1, pubKey1, []string{"usb"}, nil)
	now := time.Now()
	_ = database.ConfirmMFAMethod(method1.ID, now)

	credID2 := []byte("credential-id-2")
	pubKey2 := []byte("public-key-2")
	method2, _ := database.CreateMFAMethod(user.ID, "webauthn", "Key 2", "", credID2, pubKey2, []string{"nfc", "ble"}, nil)
	_ = database.ConfirmMFAMethod(method2.ID, now)

	methods, _ := database.ListMFAMethods(user.ID)

	waUser := &webAuthnUser{
		user:    user,
		methods: methods,
	}

	t.Run("implements WebAuthn user interface correctly", func(t *testing.T) {
		if string(waUser.WebAuthnID()) != "1" {
			t.Errorf("expected user ID '1', got '%s'", waUser.WebAuthnID())
		}

		if waUser.WebAuthnName() != user.Email {
			t.Errorf("expected name '%s', got '%s'", user.Email, waUser.WebAuthnName())
		}

		if waUser.WebAuthnDisplayName() != user.Name {
			t.Errorf("expected display name '%s', got '%s'", user.Name, waUser.WebAuthnDisplayName())
		}

		if waUser.WebAuthnIcon() != "" {
			t.Error("expected empty icon")
		}
	})

	t.Run("returns correct credentials", func(t *testing.T) {
		creds := waUser.WebAuthnCredentials()
		if len(creds) != 2 {
			t.Fatalf("expected 2 credentials, got %d", len(creds))
		}

		// Verify first credential
		if string(creds[0].ID) != string(credID1) {
			t.Error("credential ID mismatch")
		}
		if string(creds[0].PublicKey) != string(pubKey1) {
			t.Error("public key mismatch")
		}
		if len(creds[0].Transport) != 1 || creds[0].Transport[0] != protocol.USB {
			t.Error("transport mismatch")
		}

		// Verify second credential
		if string(creds[1].ID) != string(credID2) {
			t.Error("credential ID mismatch")
		}
		if len(creds[1].Transport) != 2 {
			t.Error("expected 2 transports for second credential")
		}
	})

	t.Run("filters out non-webauthn methods", func(t *testing.T) {
		// Add a TOTP method
		_, _ = database.CreateMFAMethod(user.ID, "totp", "TOTP", "secret", nil, nil, nil, nil)

		allMethods, _ := database.ListMFAMethods(user.ID)
		waUser2 := &webAuthnUser{user: user, methods: allMethods}

		creds := waUser2.WebAuthnCredentials()
		// Should still only return the 2 WebAuthn credentials
		if len(creds) != 2 {
			t.Errorf("expected 2 WebAuthn credentials despite TOTP method, got %d", len(creds))
		}
	})
}

func TestEnrollmentHelpers(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper")
	service, _ := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)

	user, _ := database.CreateUser("helper@example.com", "Helper Test", []string{"ops"})

	t.Run("ensureBackupCodes generates codes only once", func(t *testing.T) {
		// First call should generate codes
		codes1, err := service.ensureBackupCodes(user.ID)
		if err != nil {
			t.Fatalf("failed to generate backup codes: %v", err)
		}
		if len(codes1) == 0 {
			t.Error("expected backup codes to be generated")
		}

		// Second call should return nil (codes already exist)
		codes2, err := service.ensureBackupCodes(user.ID)
		if err != nil {
			t.Fatalf("failed on second call: %v", err)
		}
		if codes2 != nil {
			t.Error("expected nil codes on second call")
		}
	})

	t.Run("finalizeEnrollment confirms method and marks user enrolled", func(t *testing.T) {
		method, _ := database.CreateMFAMethod(user.ID, "totp", "Test", "secret", nil, nil, nil, nil)

		err := service.finalizeEnrollment(user.ID, method.ID)
		if err != nil {
			t.Fatalf("finalize enrollment failed: %v", err)
		}

		// Verify method is confirmed
		updatedMethod, _ := database.GetMFAMethodByID(method.ID)
		if updatedMethod.ConfirmedAt == nil {
			t.Error("expected method to be confirmed")
		}

		// Verify user is enrolled
		updatedUser, _ := database.GetUser(user.Email)
		if !updatedUser.MFAEnrolled {
			t.Error("expected user to be MFA enrolled")
		}
	})

	t.Run("prepareEnrollment deletes unconfirmed methods", func(t *testing.T) {
		// Create a fresh user for this test to avoid interference
		testUser, err := database.CreateUser("prepare-test@example.com", "Prepare Test", []string{"ops"})
		if err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		// Create an unconfirmed method
		unconfirmedMethod, err := database.CreateMFAMethod(testUser.ID, "totp", "Unconfirmed", "secret", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to create MFA method: %v", err)
		}

		// Verify it exists (use ListAllMFAMethods to include unconfirmed)
		methods1, _ := database.ListAllMFAMethods(testUser.ID)
		if len(methods1) != 1 {
			t.Fatalf("expected 1 method before cleanup, got %d", len(methods1))
		}

		err = service.prepareEnrollment(testUser.ID)
		if err != nil {
			t.Fatalf("prepare enrollment failed: %v", err)
		}

		// Unconfirmed methods should be deleted
		methods2, _ := database.ListAllMFAMethods(testUser.ID)
		if len(methods2) != 0 {
			t.Errorf("expected 0 methods after cleanup, got %d", len(methods2))
		}

		// Verify the specific unconfirmed method was deleted
		deletedMethod, _ := database.GetMFAMethodByID(unconfirmedMethod.ID)
		if deletedMethod != nil {
			t.Error("expected unconfirmed method to be deleted")
		}
	})

	t.Run("checkDuplicateYubikey detects duplicates", func(t *testing.T) {
		// Create a fresh user for this test
		testUser, err := database.CreateUser("yubikey-dup-test@example.com", "Yubikey Dup Test", []string{"ops"})
		if err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		// Use a unique public ID
		publicID := "eeeeeeeeeeee"
		_, err = database.CreateMFAMethod(testUser.ID, "yubiotp", "Yubikey Test", publicID, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("failed to create MFA method: %v", err)
		}

		// Check for duplicate should fail
		err = service.checkDuplicateYubikey(testUser.ID, publicID)
		if err == nil {
			t.Error("expected error for duplicate Yubikey")
		}
	})

	t.Run("checkDuplicateYubikey allows different keys", func(t *testing.T) {
		// Create a fresh user for this test
		testUser, _ := database.CreateUser("yubikey-diff-test@example.com", "Yubikey Diff Test", []string{"ops"})

		// Register one key
		_, _ = database.CreateMFAMethod(testUser.ID, "yubiotp", "Yubikey 1", "gggggggggggg", nil, nil, nil, nil)

		// Check a different key should succeed
		err := service.checkDuplicateYubikey(testUser.ID, "ffffffffffff")
		if err != nil {
			t.Errorf("expected no error for different key: %v", err)
		}
	})
}

func TestTimeFunction(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	pepper := []byte("test-pepper")
	service, _ := NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)

	t.Run("uses real time by default", func(t *testing.T) {
		now := service.now()
		if now.IsZero() {
			t.Error("expected non-zero time")
		}
	})

	t.Run("allows time mocking for tests", func(t *testing.T) {
		fixedTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		service.timeFunc = func() time.Time {
			return fixedTime
		}

		now := service.now()
		if !now.Equal(fixedTime) {
			t.Errorf("expected mocked time %v, got %v", fixedTime, now)
		}
	})
}
