package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

const (
	allowProtectedFlag = "allow-protected"
	allowProtectedHelp = "change protected env vars named in this command (admins only; asks for MFA every time)"
)

// errAPITokenCannotChangeProtected explains why --allow-protected fails with RACK_GATEWAY_API_TOKEN: the
// gateway only lets people change protected env vars, after MFA on the request.
var errAPITokenCannotChangeProtected = errors.New(
	"API tokens can't change protected env vars; unset RACK_GATEWAY_API_TOKEN and run the command as an admin",
)

// errProtectedEnvAppNotExplicit explains why --allow-protected without a terminal needs --app.
var errProtectedEnvAppNotExplicit = errors.New("without a terminal, --allow-protected needs an explicit --app, " +
	"so the command line in the PIN dialog names the app it changes")

// envChangeRunner is a convox env command, such as cli.EnvSet.
type envChangeRunner func(rack sdk.Interface, c *stdcli.Context) error

// runEnvChange runs a convox env command that changes keys. With --allow-protected it names keys in the
// X-Allow-Protected-Env header and always collects MFA, because the gateway verifies MFA on every protected
// env change, even within the step-up window. Without it, MFA is collected only when the step-up window has
// expired, and the gateway refuses changes to protected keys.
func runEnvChange(cmd *cobra.Command, args []string, commandName string, keys []string, run envChangeRunner) error {
	allowProtected, err := cmd.Flags().GetBool(allowProtectedFlag)
	if err != nil {
		return err
	}
	mfaAuth, err := envChangeMFAAuth(cmd, commandName, keys, allowProtected)
	if err != nil {
		return err
	}

	client, ctx, err := SetupConvoxCommandWithMFA(cmd, args, mfaAuth)
	if err != nil {
		return err
	}
	if allowProtected && len(keys) > 0 {
		client.ExtraHeaders = map[string]string{envutil.AllowProtectedHeader: strings.Join(keys, ",")}
	}
	return run(client, ctx)
}

// envChangeMFAAuth resolves the app (--app, CONVOX_APP, .convox/app or the directory name) and collects MFA for
// the change. A protected change is refused up front for API tokens, and without a terminal unless --app names
// the app: otherwise an agent could have the person at the PIN dialog approve a change to an app that the command
// line doesn't show.
func envChangeMFAAuth(cmd *cobra.Command, commandName string, keys []string, allowProtected bool) (string, error) {
	if allowProtected {
		if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
			return "", errAPITokenCannotChangeProtected
		}
		explicitApp, err := cmd.Flags().GetString("app")
		if err != nil {
			return "", err
		}
		if err := requireExplicitProtectedEnvApp(IsInteractive(), explicitApp); err != nil {
			return "", err
		}
	}
	if err := resolveAppFlag(cmd); err != nil {
		return "", err
	}
	if allowProtected {
		return protectedEnvMFAAuth(cmd, commandName, keys)
	}
	return checkMFAAndGetAuth(cmd, commandName)
}

// requireExplicitProtectedEnvApp refuses --allow-protected without a terminal (e.g. run by an AI agent) unless
// --app names the app. In a terminal the person runs the command themselves and sees the resolved app before MFA.
func requireExplicitProtectedEnvApp(interactive bool, appFlag string) error {
	if interactive || strings.TrimSpace(appFlag) != "" {
		return nil
	}
	return errProtectedEnvAppNotExplicit
}

// protectedEnvMFAAuth collects MFA for a protected env change (one prompt or key touch, or --mfa-code), even
// when the step-up window is still open. What the change does (keys, app and rack) is printed first, and shown in
// the PIN dialog, the touch prompt and the touch notification.
func protectedEnvMFAAuth(cmd *cobra.Command, commandName string, keys []string) (string, error) {
	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}
	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", err
	}
	app, err := cmd.Flags().GetString("app")
	if err != nil {
		return "", err
	}

	prompt := protectedEnvPrompt(commandName, keys, app, rack)
	for _, line := range prompt.Summary {
		if err := writeLine(cmd.ErrOrStderr(), line); err != nil {
			return "", err
		}
	}
	auth, _, err := GetMFAAuthWithPIN(cmd, gatewayURL, bearer, rack, "", prompt)
	return auth, err
}

// protectedEnvPrompt describes a protected env change for the person approving it: one summary line per key,
// each naming the app and rack (so a long list that the PIN dialog cuts short never hides where the change goes),
// and a touch reason naming the app and rack. Values are never shown.
func protectedEnvPrompt(commandName string, keys []string, app, rack string) webauthn.Prompt {
	action, verb := "Set", "set"
	if commandName == "env unset" {
		action, verb = "Unset", "unset"
	}
	target := fmt.Sprintf("on app %s (rack %s)", displayText(app), displayText(rack))

	summary := make([]string, 0, len(keys))
	for _, key := range keys {
		summary = append(summary, fmt.Sprintf("%s env var %s %s", action, displayText(key), target))
	}
	return webauthn.Prompt{Summary: summary, Touch: verb + " env vars, protected ones included, " + target}
}

// envSetKeys returns the keys of the KEY=VALUE arguments to env set, parsed the way convox's EnvSet parses
// them (it ignores arguments without '=').
func envSetKeys(args []string) []string {
	keys := make([]string, 0, len(args))
	for _, arg := range args {
		if key, _, ok := strings.Cut(arg, "="); ok {
			keys = append(keys, key)
		}
	}
	return keys
}
