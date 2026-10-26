package cli

import (
	"errors"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/cli/scriptreview"
	"github.com/DocSpring/rack-gateway/internal/cli/terminal"
	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

// reviewScript has the person running the command approve its script before anything is sent, and returns the
// security key PIN they entered with the approval, or "". API tokens run unattended: there's no one to ask.
func reviewScript(process processCommand) (string, error) {
	if process.script == nil || apiTokenOverride() != "" {
		return "", nil
	}
	return reviewScriptWith(process)
}

// reviewScriptWith shows a script for approval (scriptreview.Review), asking for the PIN as well when
// reviewPINCheck says so. Tests replace it.
var reviewScriptWith = func(process processCommand) (string, error) {
	checkPIN, err := reviewPINCheck(process)
	if err != nil {
		return "", err
	}
	return scriptreview.Review(scriptreview.Request{
		Where: process.where(), Justification: process.justification, Script: *process.script, CheckPIN: checkPIN,
		Warnings: process.envWarnings(),
	})
}

// reviewPINCheck returns the check of the PIN the review dialog asks for, so that approving the script and entering
// the security key PIN are one step, and only the touch is left for when the command runs. It returns nil, and the
// PIN is asked for when the command runs as for any other, unless the review is a dialog (no terminal), the command
// will ask for MFA (not a dry run, and no --mfa-code), the MFA method is WebAuthn, and exactly one security key is
// connected and it has a PIN (see webauthn.SingleKeyHasPIN).
func reviewPINCheck(process processCommand) (scriptreview.PINCheck, error) {
	if _, ok := terminal.StdinFD(); ok || process.dryRun {
		return nil, nil
	}
	if _, ok := inlineTotpFromFlag(); ok {
		return nil, nil
	}
	gatewayURL, bearer, err := LoadRackAuth(process.rack)
	if err != nil {
		return nil, err
	}
	status, err := loadMFAStatus(gatewayURL, bearer)
	if err != nil {
		return nil, err
	}
	method, err := selectMFAMethod(status, process.rack)
	if err != nil {
		return nil, err
	}
	if method.Type != "webauthn" || !securityKeyHasPIN() {
		return nil, nil
	}
	return checkReviewPIN, nil
}

// securityKeyHasPIN reports whether the one connected security key has a PIN (webauthn.SingleKeyHasPIN). Tests
// replace it.
var securityKeyHasPIN = webauthn.SingleKeyHasPIN

// checkReviewPIN checks a PIN entered in the review dialog on the security key, without a touch. A wrong PIN is
// asked for again.
func checkReviewPIN(pin string) (string, error) {
	err := webauthn.CheckPIN(pin)
	if errors.Is(err, webauthn.ErrWrongPIN) {
		message := err.Error()
		return strings.ToUpper(message[:1]) + message[1:] + ". Try again.", nil
	}
	return "", err
}
