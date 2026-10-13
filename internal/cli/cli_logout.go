package cli

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// LogoutCommand creates a cobra command that ends the session on the gateway, then removes the rack from the
// local config.
func LogoutCommand() *cobra.Command {
	var localOnly bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of the current rack: end the session on the gateway and remove the rack",
		Long: "Ends the rack's session on the gateway, then removes the rack (its URL and session token) from " +
			"this machine's config. If the gateway can't end the session, nothing is removed; --local removes " +
			"the rack anyway, leaving the session valid until it expires or is revoked in the web UI.",
		RunE: func(_ *cobra.Command, _ []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			return logout(rack, localOnly)
		},
	}
	cmd.Flags().BoolVar(&localOnly, "local", false,
		"only remove the rack from this machine, without ending the session on the gateway")
	return cmd
}

func logout(rack string, localOnly bool) error {
	gateway, found, err := configuredGateway(rack)
	if err != nil {
		return err
	}
	if !found {
		fmt.Printf("Rack %s not found\n", rack)
		return nil
	}

	outcome := "the session was not ended on the gateway; revoke it in the web UI"
	if !localOnly {
		outcome, err = endGatewaySession(gateway)
		if err != nil {
			return fmt.Errorf("%w\nThe rack is still configured. To remove it from this machine anyway, run "+
				"'rack-gateway logout --local --rack %s', then revoke the session in the web UI", err, rack)
		}
	}

	if _, err := RemoveRack(rack); err != nil {
		return fmt.Errorf("failed to remove rack: %w", err)
	}
	fmt.Printf("✓ Logged out from %s (%s)\n", rack, outcome)
	return nil
}

func configuredGateway(rack string) (GatewayConfig, bool, error) {
	cfg, exists, err := LoadConfig()
	if err != nil || !exists {
		return GatewayConfig{}, false, err
	}
	gateway, ok := cfg.Gateways[rack]
	return gateway, ok, nil
}

// endGatewaySession revokes the rack's session on its gateway and describes what happened. A session the
// gateway no longer accepts (expired, or already revoked) needs nothing more.
func endGatewaySession(gateway GatewayConfig) (string, error) {
	token := strings.TrimSpace(gateway.Token)
	if token == "" {
		return "there was no session", nil
	}
	gatewayURL, err := NormalizeGatewayURL(gateway.URL)
	if err != nil {
		return "", err
	}

	status, body, err := doGatewayRequest(gatewayURL, token, http.MethodPost, "/auth/cli/logout", nil)
	if err != nil {
		return "", fmt.Errorf("couldn't reach %s to end the session: %w", gatewayURL, err)
	}
	switch status {
	case http.StatusOK:
		return "the session is ended on the gateway", nil
	case http.StatusUnauthorized:
		return "the session had already ended", nil
	default:
		return "", fmt.Errorf("%s didn't end the session: %w", gatewayURL, gatewayResponseError(status, body))
	}
}
