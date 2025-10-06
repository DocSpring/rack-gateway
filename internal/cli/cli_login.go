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
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginCommandWithFlags(args, noOpen, authFile)
		},
	}

	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser automatically")
	cmd.Flags().StringVar(&authFile, "auth-file", "", "Write auth details to file for automation")

	return cmd
}

func loginCommandWithFlags(args []string, noOpen bool, authFile string) error {
	rack := args[0]
	gatewayURL := args[1]

	// Save gateway URL for this rack
	if err := SaveGatewayConfig(rack, gatewayURL); err != nil {
		return fmt.Errorf("failed to save gateway config: %w", err)
	}

	fmt.Printf("Starting login for rack: %s via gateway: %s\n", rack, gatewayURL)

	startResp, err := StartLogin(gatewayURL)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	// Always print the auth URL for users and automation
	fmt.Printf("Auth URL: %s\n", startResp.AuthURL)
	if authFile != "" {
		// Write shell-friendly lines
		content := fmt.Sprintf("AUTH_URL=%s\nSTATE=%s\nCODE_VERIFIER=%s\n", startResp.AuthURL, startResp.State, startResp.CodeVerifier)
		_ = os.WriteFile(authFile, []byte(content), 0600)
	}

	// Optionally open browser
	if !noOpen {
		fmt.Printf("Opening browser for authentication...\n")
		if err := OpenBrowser(startResp.AuthURL); err != nil {
			fmt.Printf("Please open this URL in your browser:\n%s\n", startResp.AuthURL)
		}
	}

	deviceInfo := DetermineDeviceInfo()
	// Poll the server for completion
	var loginResp *LoginResponse
	deadline := time.Now().Add(2 * time.Minute)
	pendingNotified := false
	for {
		resp, err := CompleteLogin(gatewayURL, startResp.State, startResp.CodeVerifier, deviceInfo)
		if err == nil {
			loginResp = resp
			break
		}
		if errors.Is(err, ErrLoginPending) {
			if !pendingNotified {
				fmt.Println("Waiting for multi-factor authentication to complete in your browser...")
				pendingNotified = true
			}
		} else {
			return fmt.Errorf("login failed: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("login timed out waiting for browser authentication")
		}
		time.Sleep(1 * time.Second)
	}

	// Save token
	if err := SaveToken(rack, loginResp); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	// Set as current rack
	if err := SetCurrentRack(rack); err != nil {
		return fmt.Errorf("failed to set current rack: %w", err)
	}

	fmt.Printf("✓ Successfully logged in to %s as %s\n", rack, loginResp.Email)
	return nil
}
