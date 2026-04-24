package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// LoginCommand returns the CLI command for logging into a rack via OAuth.
func LoginCommand() *cobra.Command {
	var noOpen bool
	var authFile string

	cmd := &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Long: `Login to a Convox rack via OAuth.

If no arguments are provided, re-authenticates with the current rack.
Provide a rack name to re-authenticate against a configured rack URL.
Provide both rack name and gateway URL to login to a new rack.`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			return loginCommandWithFlags(args, noOpen, authFile)
		},
	}

	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser automatically")
	cmd.Flags().StringVar(&authFile, "auth-file", "", "Write auth details to file for automation")

	return cmd
}

func loginCommandWithFlags(args []string, noOpen bool, authFile string) error {
	rack, gatewayURL, err := resolveLoginTarget(args)
	if err != nil {
		return err
	}

	fmt.Printf("Starting login for rack: %s via gateway: %s\n", rack, gatewayURL)

	startResp, err := StartLogin(gatewayURL)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	fmt.Printf("Auth URL: %s\n", startResp.AuthURL)
	if err := writeAuthFile(authFile, startResp); err != nil {
		return err
	}

	notifyBrowser(startResp.AuthURL, noOpen)

	deviceInfo := DetermineDeviceInfo()
	loginResp, err := pollLoginCompletion(gatewayURL, startResp, deviceInfo)
	if err != nil {
		return err
	}

	if err := finalizeLogin(rack, loginResp); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully logged in to %s as %s\n", rack, loginResp.Email)
	return nil
}

func resolveLoginTarget(args []string) (string, string, error) {
	rackArg, gatewayArg := normalizeLoginArgs(args)
	switch len(args) {
	case 0:
		rack, err := SelectedRack()
		if err != nil {
			return "", "", fmt.Errorf("no current rack selected: %w. Run: rack-gateway login <rack> <gateway-url>", err)
		}
		gatewayURL, err := resolveLoginGatewayURL(rack)
		if err != nil {
			return "", "", fmt.Errorf(
				"rack %s not configured: %w. Run: rack-gateway login <rack> <gateway-url>",
				rack,
				err,
			)
		}
		return rack, gatewayURL, nil
	case 1:
		rack := rackArg
		if rack == "" {
			return "", "", fmt.Errorf("rack name cannot be empty")
		}
		gatewayURL, err := resolveLoginGatewayURL(rack)
		if err != nil {
			return "", "", fmt.Errorf("gateway URL required for rack %s: %w", rack, err)
		}
		return rack, gatewayURL, nil
	case 2:
		rack := rackArg
		gatewayURL := gatewayArg
		if rack == "" {
			return "", "", fmt.Errorf("rack name cannot be empty")
		}
		if gatewayURL == "" {
			return "", "", fmt.Errorf("gateway URL cannot be empty")
		}
		if err := SaveGatewayConfig(rack, gatewayURL); err != nil {
			return "", "", fmt.Errorf("failed to save gateway config: %w", err)
		}
		return rack, gatewayURL, nil
	default:
		return "", "", fmt.Errorf("unexpected number of arguments")
	}
}

func normalizeLoginArgs(args []string) (string, string) {
	var rack string
	var gatewayURL string
	for i, arg := range args {
		switch i {
		case 0:
			rack = strings.TrimSpace(arg)
		case 1:
			gatewayURL = strings.TrimSpace(arg)
		}
	}
	return rack, gatewayURL
}

func resolveLoginGatewayURL(rack string) (string, error) {
	if gatewayURL := strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL")); gatewayURL != "" {
		if err := SaveGatewayConfig(rack, gatewayURL); err != nil {
			return "", fmt.Errorf("failed to save gateway config: %w", err)
		}
		return gatewayURL, nil
	}
	return LoadGatewayURL(rack)
}

func writeAuthFile(path string, startResp *LoginStartResponse) error {
	if path == "" {
		return nil
	}
	content := fmt.Sprintf(
		"AUTH_URL=%s\nSTATE=%s\nCODE_VERIFIER=%s\n",
		startResp.AuthURL,
		startResp.State,
		startResp.CodeVerifier,
	)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}
	return nil
}

func notifyBrowser(authURL string, noOpen bool) {
	if noOpen {
		return
	}
	fmt.Printf("Opening browser for authentication...\n")
	if err := OpenBrowser(authURL); err != nil {
		fmt.Printf("Please open this URL in your browser:\n%s\n", authURL)
	}
}

func pollLoginCompletion(
	gatewayURL string,
	startResp *LoginStartResponse,
	deviceInfo DeviceInfo,
) (*LoginResponse, error) {
	deadline := time.Now().Add(2 * time.Minute)
	pendingNotified := false
	for {
		resp, err := CompleteLogin(gatewayURL, startResp.State, startResp.CodeVerifier, deviceInfo)
		if err == nil {
			return resp, nil
		}

		if !errors.Is(err, ErrLoginPending) {
			return nil, fmt.Errorf("login failed: %w", err)
		}

		if !pendingNotified {
			fmt.Println("Waiting for multi-factor authentication to complete in your browser...")
			pendingNotified = true
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("login timed out waiting for browser authentication")
		}
		time.Sleep(1 * time.Second)
	}
}

func finalizeLogin(rack string, loginResp *LoginResponse) error {
	if err := SaveToken(rack, loginResp); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}
	if err := SetCurrentRack(rack); err != nil {
		return fmt.Errorf("failed to set current rack: %w", err)
	}
	return nil
}
