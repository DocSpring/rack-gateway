package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The PIN entered in the review dialog is kept for the command's MFA proof, so the PIN dialog doesn't ask again.
func TestProcessCommandKeepsThePINFromTheReview(t *testing.T) {
	newEnvChangeGateway(t)
	previous := reviewScriptWith
	reviewScriptWith = func(processCommand) (string, error) { return "123456", nil }
	t.Cleanup(func() { reviewScriptWith = previous })
	flags := map[string]string{scriptFlag: writeScript(t, "count.rb", "puts 1\n"), justificationFlag: testJustification}

	process, err := prepareProcessCommand(processCobraCommand(t, RunCommand(), flags),
		[]string{"web", "rails", "runner"}, "run")
	require.NoError(t, err)
	require.Equal(t, "123456", process.pin)
}

// The review dialog asks for the PIN only when the command will ask for a WebAuthn proof and the one connected
// security key has a PIN: not for another MFA method, a dry run, or a TOTP code given with --mfa-code.
func TestReviewPINCheck(t *testing.T) {
	gateway := newEnvChangeGateway(t)
	hasPIN := true
	previous := securityKeyHasPIN
	securityKeyHasPIN = func() bool { return hasPIN }
	t.Cleanup(func() { securityKeyHasPIN = previous })
	rack, err := SelectedRack()
	require.NoError(t, err)
	process := processCommand{rack: rack}
	asksForPIN := func() bool {
		t.Helper()
		check, err := reviewPINCheck(process)
		require.NoError(t, err)
		return check != nil
	}

	require.False(t, asksForPIN(), "a TOTP method")

	gateway.mu.Lock()
	gateway.mfaMethods = []MFAMethodResponse{{ID: 2, Type: "webauthn", Label: "YubiKey"}}
	gateway.mu.Unlock()
	require.True(t, asksForPIN())

	hasPIN = false
	require.False(t, asksForPIN(), "several keys, or a key without a PIN")
	hasPIN = true

	process.dryRun = true
	require.False(t, asksForPIN(), "a dry run asks for no MFA")
	process.dryRun = false

	MFACodeFlag = "123456"
	require.False(t, asksForPIN(), "a TOTP code given with --mfa-code")
}
