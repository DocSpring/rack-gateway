package cli

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

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
	cmd.Flags().StringVar(&authFile, "auth-file", "", "Write the login URL to file for automation")

	return cmd
}

func loginCommandWithFlags(args []string, noOpen bool, authFile string) error {
	rack, gatewayURL, err := resolveLoginTarget(args)
	if err != nil {
		return err
	}

	fmt.Printf("Starting login for rack: %s via gateway: %s\n", rack, gatewayURL)

	loginResp, err := runLoopbackLogin(gatewayURL, noOpen, authFile)
	if err != nil {
		return err
	}

	if err := finalizeLogin(rack, loginResp); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully logged in to %s as %s\n", rack, loginResp.Email)
	return nil
}

// runLoopbackLogin performs an RFC 8252 loopback login: the browser hands a single-use login code
// back to this process on 127.0.0.1, and only this process holds the PKCE verifier to redeem it.
func runLoopbackLogin(gatewayURL string, noOpen bool, authFile string) (*LoginResponse, error) {
	challenge, err := newPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}

	loopback, err := startLoopbackServer(state, gatewayURL)
	if err != nil {
		return nil, err
	}
	defer loopback.close()

	deviceInfo := DetermineDeviceInfo()
	startResp, err := StartLogin(gatewayURL, LoginStartRequest{
		CodeChallenge:       challenge.challenge,
		CodeChallengeMethod: "S256",
		RedirectURI:         loopback.redirectURI,
		State:               state,
		DeviceName:          deviceInfo.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start login: %w", err)
	}
	if err := validateAuthURL(startResp.AuthURL, gatewayURL); err != nil {
		return nil, err
	}
	if err := writeAuthFile(authFile, startResp); err != nil {
		return nil, err
	}

	notifyBrowser(startResp.AuthURL, noOpen)
	fmt.Println("Waiting for you to finish logging in in your browser...")

	loginCode, err := loopback.wait(loginTimeout)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	loginResp, err := CompleteLogin(gatewayURL, loginCode, challenge.verifier, deviceInfo)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}
	return loginResp, nil
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

// writeAuthFile records the login URL for automation (e.g. E2E tests drive the browser step).
// It never contains the PKCE verifier or the CLI's state.
func writeAuthFile(path string, startResp *LoginStartResponse) error {
	if path == "" {
		return nil
	}
	content := fmt.Sprintf("AUTH_URL=%s\n", startResp.AuthURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}
	return nil
}

func notifyBrowser(authURL string, noOpen bool) {
	if noOpen {
		fmt.Printf("Open this URL in your browser to log in:\n%s\n", authURL)
		return
	}
	fmt.Printf("Opening browser for authentication...\n")
	if err := OpenBrowser(authURL); err != nil {
		fmt.Printf("Please open this URL in your browser:\n%s\n", authURL)
	}
}

// validateAuthURL only lets the CLI open Google's sign-in page, or a loopback identity provider
// when the gateway itself is on a loopback address (local development and tests).
func validateAuthURL(authURL, gatewayURL string) error {
	parsed, err := url.Parse(authURL)
	if err != nil {
		return fmt.Errorf("gateway returned an invalid login URL: %w", err)
	}
	if parsed.Scheme == "https" && parsed.Hostname() == "accounts.google.com" {
		return nil
	}
	gateway, err := url.Parse(buildGatewayAPIURL(gatewayURL, ""))
	if err == nil && isLoopbackHost(gateway.Hostname()) && isLoopbackHost(parsed.Hostname()) &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") {
		return nil
	}
	return fmt.Errorf("gateway returned an unexpected login URL host %q", parsed.Host)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
