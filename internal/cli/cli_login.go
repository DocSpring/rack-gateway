package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func LoginCommand() *cobra.Command {
	var noOpen bool
	var authFile string

	cmd := &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Long: `Login to a Convox rack via OAuth.

If no arguments are provided, re-authenticates with the current rack.
Otherwise, provide both rack name and gateway URL to login to a new rack.`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
	switch len(args) {
	case 0:
		rack, err := SelectedRack()
		if err != nil {
			return "", "", fmt.Errorf("no current rack selected: %w. Run: rack-gateway login <rack> <gateway-url>", err)
		}
		gatewayURL, _, err := LoadRackAuth(rack)
		if err != nil {
			return "", "", fmt.Errorf("rack %s not configured: %w. Run: rack-gateway login <rack> <gateway-url>", rack, err)
		}
		return rack, gatewayURL, nil
	case 1:
		return "", "", fmt.Errorf("both rack name and gateway URL are required")
	case 2:
		rack, gatewayURL := args[0], args[1]
		if err := SaveGatewayConfig(rack, gatewayURL); err != nil {
			return "", "", fmt.Errorf("failed to save gateway config: %w", err)
		}
		return rack, gatewayURL, nil
	default:
		return "", "", fmt.Errorf("unexpected number of arguments")
	}
}

func writeAuthFile(path string, startResp *LoginStartResponse) error {
	if path == "" {
		return nil
	}
	content := fmt.Sprintf("AUTH_URL=%s\nSTATE=%s\nCODE_VERIFIER=%s\n", startResp.AuthURL, startResp.State, startResp.CodeVerifier)
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

func pollLoginCompletion(gatewayURL string, startResp *LoginStartResponse, deviceInfo DeviceInfo) (*LoginResponse, error) {
	deadline := time.Now().Add(2 * time.Minute)
	pendingNotified := false
	for {
		resp, err := CompleteLogin(gatewayURL, startResp.State, startResp.CodeVerifier, deviceInfo)
		if err == nil {
			return resp, nil
		}

		if errors.Is(err, ErrLoginPending) {
			if !pendingNotified {
				fmt.Println("Waiting for multi-factor authentication to complete in your browser...")
				pendingNotified = true
			}
		} else {
			return nil, fmt.Errorf("login failed: %w", err)
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
