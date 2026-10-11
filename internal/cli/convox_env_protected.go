package cli

import (
	"errors"
	"os"
	"strings"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"

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

// envChangeRunner is a convox env command, such as cli.EnvSet.
type envChangeRunner func(rack sdk.Interface, c *stdcli.Context) error

// runEnvChange runs a convox env command that changes keys. With --allow-protected it names keys in the
// X-Allow-Protected-Env header and always collects MFA, because the gateway verifies MFA on every protected
// env change, even within the step-up window. Without it, MFA is collected only when the step-up window has
// expired, and the gateway refuses changes to protected keys.
func runEnvChange(cmd *cobra.Command, args []string, commandName string, keys []string, run envChangeRunner) error {
	if err := resolveAppFlag(cmd); err != nil {
		return err
	}
	allowProtected, err := cmd.Flags().GetBool(allowProtectedFlag)
	if err != nil {
		return err
	}

	var mfaAuth string
	if allowProtected {
		mfaAuth, err = protectedEnvMFAAuth(cmd)
	} else {
		mfaAuth, err = checkMFAAndGetAuth(cmd, commandName)
	}
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

// protectedEnvMFAAuth collects MFA for a protected env change (one prompt or key touch, or --mfa-code), even
// when the step-up window is still open.
func protectedEnvMFAAuth(cmd *cobra.Command) (string, error) {
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		return "", errAPITokenCannotChangeProtected
	}
	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}
	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", err
	}
	return promptMFAForCommand(cmd, gatewayURL, bearer, rack)
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
