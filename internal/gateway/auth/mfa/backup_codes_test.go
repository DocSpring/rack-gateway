package mfa

import (
	"strings"
	"testing"
)

func TestNormalizeBackupCode(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"ABCDEF012345",
		"abcdef012345",
		"abcdef-012345",
		" ABC DEF 012 345 ",
		"ab-cd-ef\t01-23-45",
	}
	for _, input := range inputs {
		if got := normalizeBackupCode(input); got != "ABCDEF012345" {
			t.Errorf("normalizeBackupCode(%q) = %q, want ABCDEF012345", input, got)
		}
	}
}

func TestVerifyTOTP_AcceptsBackupCodeOnceInAnyFormat(t *testing.T) {
	t.Parallel()

	svc, _, user := setupMFAService(t, "backup@example.com", "Backup User")
	codes, err := svc.GenerateBackupCodes(user.ID)
	if err != nil {
		t.Fatalf("failed to generate backup codes: %v", err)
	}
	code := codes[0]
	typed := strings.ToLower(code[:6]) + "-" + strings.ToLower(code[6:])

	result, err := svc.VerifyTOTP(user, typed, "1.2.3.4", "test-agent", nil)
	if err != nil {
		t.Fatalf("expected backup code %q to verify, got: %v", typed, err)
	}
	if result == nil || result.MethodID != 0 {
		t.Fatalf("expected a backup-code verification result, got %+v", result)
	}

	if _, err := svc.VerifyTOTP(user, code, "1.2.3.4", "test-agent", nil); err == nil {
		t.Fatal("expected a used backup code to be rejected")
	}
}
