package mfa

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func backupCodeHashes(t *testing.T, database *db.Database, userID int64) []string {
	t.Helper()

	codes, err := database.ListBackupCodes(userID)
	if err != nil {
		t.Fatalf("failed to list backup codes: %v", err)
	}
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hashes = append(hashes, code.CodeHash)
	}
	return hashes
}

func enrollTOTP(t *testing.T, svc *Service, database *db.Database, user *db.User) *db.User {
	t.Helper()

	start, err := svc.StartTOTPEnrollment(user)
	if err != nil {
		t.Fatalf("failed to start TOTP enrollment: %v", err)
	}
	code, err := totp.GenerateCode(start.Secret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}
	if err := svc.ConfirmTOTP(user, start.MethodID, code); err != nil {
		t.Fatalf("failed to confirm TOTP: %v", err)
	}
	enrolled, err := database.GetUser(user.Email)
	if err != nil || enrolled == nil || !enrolled.MFAEnrolled {
		t.Fatalf("expected user to be enrolled, err=%v", err)
	}
	return enrolled
}

func TestStartTOTPEnrollment_KeepsEnrolledUsersBackupCodes(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "keep-codes@example.com", "Keep Codes")
	enrolled := enrollTOTP(t, svc, database, user)
	before := backupCodeHashes(t, database, enrolled.ID)
	if len(before) == 0 {
		t.Fatal("expected backup codes after first enrollment")
	}

	result, err := svc.StartTOTPEnrollment(enrolled)
	if err != nil {
		t.Fatalf("failed to start second TOTP enrollment: %v", err)
	}
	if len(result.BackupCodes) != 0 {
		t.Fatalf("expected no backup codes for an enrolled user, got %d", len(result.BackupCodes))
	}

	after := backupCodeHashes(t, database, enrolled.ID)
	if len(after) != len(before) {
		t.Fatalf("backup code count changed: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("existing backup codes were replaced by a second enrollment")
		}
	}
}

func TestStartTOTPEnrollment_RestartedFirstEnrollmentIssuesFreshCodes(t *testing.T) {
	t.Parallel()

	svc, database, user := setupMFAService(t, "first-enroll@example.com", "First Enroll")

	first, err := svc.StartTOTPEnrollment(user)
	if err != nil {
		t.Fatalf("failed to start enrollment: %v", err)
	}
	if len(first.BackupCodes) == 0 {
		t.Fatal("expected backup codes on first enrollment")
	}

	second, err := svc.StartTOTPEnrollment(user)
	if err != nil {
		t.Fatalf("failed to restart enrollment: %v", err)
	}
	if len(second.BackupCodes) == 0 {
		t.Fatal("expected a restarted first enrollment to show backup codes again")
	}
	if len(backupCodeHashes(t, database, user.ID)) != len(second.BackupCodes) {
		t.Fatal("stored backup codes should match the latest set shown to the user")
	}
}
