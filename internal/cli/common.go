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
)

// Helper functions for convox commands

// buildRackURL constructs a RACK_URL in the format: https://convox:AUTH@gateway/api/v1/convox
// AUTH format: JWT or JWT.mfa_type.mfa_value (using dots to avoid URL encoding issues)
// Examples:
//   - No MFA: eyJhbGc...
//   - TOTP:   eyJhbGc....totp.123456
//   - WebAuthn: eyJhbGc....webauthn.base64_assertion
func buildRackURL(gatewayURL, auth string) string {
	// Add /api/v1/convox prefix to the gateway URL
	base := strings.TrimSuffix(gatewayURL, "/") + "/api/v1/convox"

	// Inject auth as basic auth password
	if strings.HasPrefix(base, "http://") {
		return fmt.Sprintf("http://convox:%s@%s", auth, strings.TrimPrefix(base, "http://"))
	}
	return fmt.Sprintf("https://convox:%s@%s", auth, strings.TrimPrefix(base, "https://"))
}

// SetupConvoxCommand sets up a convox command with the standard boilerplate
// Pass flagNames to convert specific cobra flags to stdcli flags
func SetupConvoxCommand(cobraCmd *cobra.Command, args []string, flagNames ...string) (*sdk.Client, *stdcli.Context, error) {
	return SetupConvoxCommandWithMFA(cobraCmd, args, "", flagNames...)
}

// SetupConvoxCommandWithMFA sets up a convox command with optional MFA verification
// mfaAuth should be in format "totp.123456" or "webauthn.assertion_data" or empty string for no MFA
func SetupConvoxCommandWithMFA(cobraCmd *cobra.Command, args []string, mfaAuth string, flagNames ...string) (*sdk.Client, *stdcli.Context, error) {
	rack, err := SelectedRack()
	if err != nil {
		return nil, nil, err
	}

	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return nil, nil, err
	}

	// Build auth string: JWT or JWT.mfa_type.mfa_value (using dots to avoid URL encoding issues)
	auth := token
	if mfaAuth != "" {
		auth = token + "." + mfaAuth
	}

	// Build RACK_URL with auth as password
	rackURL := buildRackURL(gatewayURL, auth)

	// Use the real convox SDK
	client, err := sdk.New(rackURL)
	if err != nil {
		return nil, nil, err
	}

	// Create stdcli engine
	engine := stdcli.New("rack-gateway", Version)

	// Create a new Writer with custom tags to avoid modifying the global DefaultWriter
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

	// Build flags - convert specified cobra flags to stdcli flags
	var flags []*stdcli.Flag
	for _, name := range flagNames {
		if val, _ := cobraCmd.Flags().GetString(name); val != "" {
			flags = append(flags, &stdcli.Flag{Name: name, Value: val})
		}
	}

	// Create stdcli context
	ctx := &stdcli.Context{
		Context: cobraCmd.Context(),
		Args:    args,
		Flags:   flags,
	}

	// Use unsafe to set private engine field
	ctxValue := reflect.ValueOf(ctx).Elem()
	engineField := ctxValue.FieldByName("engine")
	if engineField.IsValid() {
		reflect.NewAt(engineField.Type(), unsafe.Pointer(engineField.UnsafeAddr())).
			Elem().
			Set(reflect.ValueOf(engine))
	}

	return client, ctx, nil
}
