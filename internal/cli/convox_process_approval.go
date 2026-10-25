package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// justificationFlag says why a command (exec, run) is run and what it does. The gateway records it with the
// command, and an app can require one for every command.
const justificationFlag = "justification"

// addJustificationFlag adds --justification to the exec or run command cmd.
func addJustificationFlag(cmd *cobra.Command) {
	cmd.Flags().String(justificationFlag, "", fmt.Sprintf(
		"why the command is run and what it does (%d to %d characters, one line), recorded in the audit log",
		audit.MinJustificationChars, audit.MaxJustificationChars))
}

// commandJustification returns --justification, checked, or "" without one. A command without one in an app that
// requires one is refused here, before anything starts: the gateway refuses it too, but the CLI can't show why the
// gateway refused an exec's WebSocket handshake.
func commandJustification(cmd *cobra.Command, app string) (string, error) {
	text, err := cmd.Flags().GetString(justificationFlag)
	if err != nil {
		return "", err
	}
	if cmd.Flags().Changed(justificationFlag) {
		if err := audit.CheckJustification(text); err != nil {
			return "", err
		}
		return strings.TrimSpace(text), nil
	}
	required, err := appRequiresJustification(cmd, app)
	if err != nil {
		return "", err
	}
	if required {
		return "", fmt.Errorf("app %s requires a justification for every command: add --justification, saying "+
			"why the command is run and what it does (at least %d characters)",
			displayText(app), audit.MinJustificationChars)
	}
	return "", nil
}

// appRequiresJustification reads the app's require_command_justification setting. API tokens can't read settings;
// the gateway still refuses their commands without one (deploy approvals justify theirs with the approval).
func appRequiresJustification(cmd *cobra.Command, app string) (bool, error) {
	if apiTokenOverride() != "" {
		return false, nil
	}
	rack, err := SelectedRack()
	if err != nil {
		return false, err
	}
	var appSettings map[string]struct {
		Value interface{} `json:"value"`
	}
	path := "/apps/" + url.PathEscape(app) + "/settings"
	if err := gatewayRequest(cmd, rack, http.MethodGet, path, nil, &appSettings); err != nil {
		return false, fmt.Errorf("failed to read whether app %s requires a justification: %w", displayText(app), err)
	}
	required, _ := appSettings[settings.KeyRequireCommandJustification].Value.(bool)
	return required, nil
}

// commandPrompt describes a command for the person approving it with MFA: where it runs, the command (a script's
// words and file name), its justification and its script's size and SHA-256, in the PIN dialog, the touch prompt
// and the touch notification.
func commandPrompt(process processCommand) webauthn.Prompt {
	verb := "run in a new " + displayText(process.target) + " process"
	if process.action == "exec" {
		verb = "exec in process " + displayText(process.target)
	}
	summary := []string{fmt.Sprintf("On %s, %s: %s", process.where(), verb, displayText(process.shownCommand()))}
	if process.justification != "" {
		summary = append(summary, "Justification: "+displayText(process.justification))
	}
	if script := process.script; script != nil {
		summary = append(summary, fmt.Sprintf("Script: %s, %d lines, SHA-256 %s", script.Name, script.Lines(),
			script.SHA256))
	}
	return webauthn.Prompt{Summary: summary, Touch: "run a command on " + process.where()}
}

// freshMFAAuth collects an MFA proof for the selected rack (one prompt or key touch, or --mfa-code), whatever the
// step-up window says, with prompt describing what it approves. API tokens have no MFA.
func freshMFAAuth(cmd *cobra.Command, prompt webauthn.Prompt, pin string) (string, error) {
	if apiTokenOverride() != "" {
		return "", nil
	}
	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}
	gatewayURL, bearer, err := LoadRackAuth(rack)
	if err != nil {
		return "", err
	}
	auth, _, err := GetMFAAuthWithPIN(cmd, gatewayURL, bearer, rack, pin, prompt)
	return auth, err
}
