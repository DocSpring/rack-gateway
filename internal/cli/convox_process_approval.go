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
// requires one (required) is refused here, before any MFA prompt or process start: the gateway refuses it too.
func commandJustification(cmd *cobra.Command, app string, required bool) (string, error) {
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
	if required {
		return "", fmt.Errorf("app %s requires a justification for every command: add --justification, saying "+
			"why the command is run and what it does (at least %d characters)",
			displayText(app), audit.MinJustificationChars)
	}
	return "", nil
}

// commandSettings is what the gateway's settings say about the commands run in an app: whether each needs a
// justification (the gateway-wide require_command_justification, or the app's own), and the env vars they get.
// API tokens can't read settings, so known is false; the gateway still checks their commands.
type commandSettings struct {
	known                 bool
	justificationRequired bool
	env                   settings.CommandEnv
}

// settingValues is a settings response: each setting's value.
type settingValues map[string]struct {
	Value interface{} `json:"value"`
}

// readCommandSettings reads the settings that apply to commands run in app.
func readCommandSettings(cmd *cobra.Command, app string) (commandSettings, error) {
	if apiTokenOverride() != "" {
		return commandSettings{}, nil
	}
	rack, err := SelectedRack()
	if err != nil {
		return commandSettings{}, err
	}
	var global, values settingValues
	for path, into := range map[string]*settingValues{
		"/settings": &global, "/apps/" + url.PathEscape(app) + "/settings": &values,
	} {
		if err := gatewayRequest(cmd, rack, http.MethodGet, path, nil, into); err != nil {
			return commandSettings{}, fmt.Errorf("failed to read the settings of app %s's commands: %w",
				displayText(app), err)
		}
	}
	required := func(values settingValues) bool {
		on, _ := values[settings.KeyRequireCommandJustification].Value.(bool)
		return on
	}
	env, err := settings.CommandEnvFromValues(
		valueOr(values, settings.KeyCommandEnvVars, []interface{}{}),
		valueOr(values, settings.KeyCommandEnvReadOnly, map[string]interface{}{}),
	)
	if err != nil {
		return commandSettings{}, fmt.Errorf("app %s: %w", displayText(app), err)
	}
	return commandSettings{known: true, justificationRequired: required(global) || required(values), env: env}, nil
}

// valueOr returns the value of setting key, or empty when it's unset (null).
func valueOr(values settingValues, key string, empty interface{}) interface{} {
	if value := values[key].Value; value != nil {
		return value
	}
	return empty
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
	if env := process.envSummary(); env != "" {
		summary = append(summary, env)
	}
	touch := "run a command on "
	if process.canChangeData() {
		touch = "run a command that can change data on "
	}
	return webauthn.Prompt{Summary: summary, Warnings: process.envWarnings(), Touch: touch + process.where()}
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
