package cli

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"unsafe"

	"github.com/convox/convox/sdk"
	"github.com/convox/stdcli"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Helper functions for convox commands

// buildRackURL constructs a RACK_URL in the format: https://convox:AUTH@gateway/api/v1/rack-proxy
// AUTH format: session_token or session_token.mfa_type.mfa_value (using dots to avoid URL encoding issues)
// Examples:
//   - No MFA: abc123def456...
//   - TOTP:   abc123def456....totp.123456
//   - WebAuthn: abc123def456....webauthn.base64_assertion
func buildRackURL(gatewayURL, auth string) string {
	// Add /api/v1/rack-proxy prefix to the gateway URL
	base := strings.TrimSuffix(gatewayURL, "/") + "/api/v1/rack-proxy"

	// Inject auth as basic auth password
	if strings.HasPrefix(base, "http://") {
		return fmt.Sprintf("http://convox:%s@%s", auth, strings.TrimPrefix(base, "http://"))
	}
	return fmt.Sprintf("https://convox:%s@%s", auth, strings.TrimPrefix(base, "https://"))
}

// Global flags that should NEVER be forwarded to the Convox SDK
var globalFlagsToExclude = map[string]bool{
	"config":     true, // Config directory path
	"rack":       true, // Rack selection
	"api-token":  true, // API token for CLI requests
	"mfa-code":   true, // MFA code for step-up auth
	"mfa-method": true, // MFA method selection
	"help":       true, // Help flag
}

// SetupConvoxCommand sets up a convox command with the standard boilerplate
// Automatically forwards ALL non-global flags to the Convox SDK
func SetupConvoxCommand(
	cobraCmd *cobra.Command,
	args []string,
) (*sdk.Client, *stdcli.Context, error) {
	return SetupConvoxCommandWithMFA(cobraCmd, args, "")
}

// SetupConvoxCommandWithMFA sets up a convox command with optional MFA verification
// mfaAuth should be in format "totp.123456" or "webauthn.assertion_data" or empty string for no MFA
// Automatically forwards ALL non-global flags to the Convox SDK
func SetupConvoxCommandWithMFA(
	cobraCmd *cobra.Command,
	args []string,
	mfaAuth string,
) (*sdk.Client, *stdcli.Context, error) {
	_, gatewayURL, auth, err := resolveRackAuth(mfaAuth)
	if err != nil {
		return nil, nil, err
	}

	client, err := sdk.New(buildRackURL(gatewayURL, auth))
	if err != nil {
		return nil, nil, err
	}

	engine := newStdCLIEngine(cobraCmd)
	flags := collectAllNonGlobalFlags(cobraCmd)
	ctx := newStdCLIContext(cobraCmd, args, flags)
	injectStdCLIEngine(ctx, engine)

	return client, ctx, nil
}

func resolveRackAuth(mfaAuth string) (string, string, string, error) {
	rack, err := SelectedRack()
	if err != nil {
		return "", "", "", err
	}

	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return "", "", "", err
	}

	if mfaAuth == "" {
		return rack, gatewayURL, token, nil
	}
	return rack, gatewayURL, token + "." + mfaAuth, nil
}

func newStdCLIEngine(cobraCmd *cobra.Command) *stdcli.Engine {
	engine := stdcli.New("rack-gateway", Version)
	stdout := cobraCmd.OutOrStdout()
	isTerminal := false
	if f, ok := stdout.(*os.File); ok {
		isTerminal = stdcli.IsTerminal(f)
	}
	engine.Writer = &stdcli.Writer{
		Color:  isTerminal,
		Stdout: stdout,
		Stderr: cobraCmd.ErrOrStderr(),
		Tags: map[string]stdcli.Renderer{
			"app":      stdcli.RenderColors(39),
			"command":  stdcli.RenderColors(244),
			"dir":      stdcli.RenderColors(246),
			"build":    stdcli.RenderColors(23),
			"fail":     stdcli.RenderColors(160),
			"rack":     stdcli.RenderColors(26),
			"process":  stdcli.RenderColors(27),
			"release":  stdcli.RenderColors(24),
			"resource": stdcli.RenderColors(33),
			"service":  stdcli.RenderColors(33),
			"setting":  stdcli.RenderColors(246),
			"system":   stdcli.RenderColors(15),
			"error":    stdcli.DefaultWriter.Tags["error"],
			"header":   stdcli.RenderColors(242),
			"h1":       stdcli.RenderColors(244),
			"h2":       stdcli.RenderColors(241),
			"id":       stdcli.RenderColors(247),
			"info":     stdcli.RenderColors(247),
			"ok":       stdcli.RenderColors(46),
			"start":    stdcli.RenderColors(247),
			"u":        stdcli.RenderUnderline(),
			"value":    stdcli.RenderColors(251),
		},
	}
	return engine
}

// collectAllNonGlobalFlags automatically collects all flags except global ones
// This ensures that any flag defined on a command is automatically forwarded to the Convox SDK
func collectAllNonGlobalFlags(cobraCmd *cobra.Command) []*stdcli.Flag {
	flags := make([]*stdcli.Flag, 0)

	cobraCmd.Flags().VisitAll(func(cobraFlag *pflag.Flag) {
		// Skip global flags
		if globalFlagsToExclude[cobraFlag.Name] {
			return
		}

		// Skip if flag wasn't changed from default (no value provided)
		if !cobraFlag.Changed {
			return
		}

		// Convert and collect the flag
		flag, kind := convertFlagValue(cobraCmd, cobraFlag.Name, cobraFlag.Value.Type())
		if flag == nil {
			return
		}
		applyFlagKind(flag, kind)
		flags = append(flags, flag)
	})

	return flags
}

func convertFlagValue(cmd *cobra.Command, name, flagType string) (*stdcli.Flag, string) {
	switch flagType {
	case "bool":
		if val, _ := cmd.Flags().GetBool(name); val {
			return &stdcli.Flag{Name: name, Value: val}, "bool"
		}
	case "int":
		if val, _ := cmd.Flags().GetInt(name); val != 0 {
			return &stdcli.Flag{Name: name, Value: val}, "int"
		}
	case "stringSlice":
		if val, _ := cmd.Flags().GetStringSlice(name); len(val) > 0 {
			return &stdcli.Flag{Name: name, Value: val}, "stringslice"
		}
	default:
		if val, _ := cmd.Flags().GetString(name); val != "" {
			return &stdcli.Flag{Name: name, Value: val}, "string"
		}
	}
	return nil, ""
}

func applyFlagKind(flag *stdcli.Flag, kind string) {
	if flag == nil || kind == "" {
		return
	}
	flagValue := reflect.ValueOf(flag).Elem()
	kindField := flagValue.FieldByName("kind")
	if kindField.IsValid() {
		reflect.NewAt(kindField.Type(), unsafe.Pointer(kindField.UnsafeAddr())).
			Elem().
			SetString(kind)
	}
}

func newStdCLIContext(cobraCmd *cobra.Command, args []string, flags []*stdcli.Flag) *stdcli.Context {
	return &stdcli.Context{
		Context: cobraCmd.Context(),
		Args:    args,
		Flags:   flags,
	}
}

func injectStdCLIEngine(ctx *stdcli.Context, engine *stdcli.Engine) {
	ctxValue := reflect.ValueOf(ctx).Elem()
	engineField := ctxValue.FieldByName("engine")
	if engineField.IsValid() {
		reflect.NewAt(engineField.Type(), unsafe.Pointer(engineField.UnsafeAddr())).
			Elem().
			Set(reflect.ValueOf(engine))
	}
}

func setupConvoxWithMFAAction(
	cobraCmd *cobra.Command,
	args []string,
	action string,
) (*sdk.Client, *stdcli.Context, error) {
	mfaAuth, err := checkMFAAndGetAuth(cobraCmd, action)
	if err != nil {
		return nil, nil, err
	}
	return SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
}

func normalizeConvoxExit(err error) error {
	if err != nil && err.Error() == "exit 0" {
		return nil
	}
	return err
}
