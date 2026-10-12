package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// GatewayCommand creates the gateway command (shows gateway server info)
func GatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Show gateway server information",
		Long:  "Display information about the connected rack-gateway server",
		Args:  cobra.NoArgs,
		RunE:  SilenceOnError(runGatewayCommand),
	}
}

func runGatewayCommand(_ *cobra.Command, _ []string) error {
	rack, err := SelectedRack()
	if err != nil {
		return fmt.Errorf("not logged in: %w", err)
	}

	gatewayURL, token, err := LoadRackAuth(rack)
	if err != nil {
		return fmt.Errorf("not logged in: %w", err)
	}

	info, err := GetGatewayInfo(gatewayURL, token)
	if err != nil {
		return fmt.Errorf("failed to get gateway info: %w", err)
	}

	printGatewayInfo(gatewayURL, info)
	return nil
}

// printGatewayInfo prints what the gateway reports about itself, its rack and the user. Every value comes from the
// gateway, so each goes through displayText.
func printGatewayInfo(gatewayURL string, info *GatewayInfoResponse) {
	// Gateway info
	fmt.Printf("Gateway URL:     %s\n", gatewayURL)
	if info.Version.Version != "" {
		fmt.Printf("Gateway Version: %s\n", gatewayVersion(info))
	}

	// Rack info
	fmt.Println()
	fmt.Printf("Rack Name:       %s\n", displayText(info.Rack.Name))
	if info.Rack.Alias != "" && info.Rack.Alias != info.Rack.Name {
		fmt.Printf("Rack Alias:      %s\n", displayText(info.Rack.Alias))
	}
	if info.Rack.Host != "" {
		fmt.Printf("Rack Host:       %s\n", displayText(info.Rack.Host))
	}

	// User info
	fmt.Println()
	fmt.Printf("Logged in as:    %s\n", displayText(info.User.Email))
	if info.User.Name != "" {
		fmt.Printf("Name:            %s\n", displayText(info.User.Name))
	}
	if len(info.User.Roles) > 0 {
		fmt.Printf("Roles:           %s\n", displayText(strings.Join(info.User.Roles, ", ")))
	}

	// Integrations
	printIntegrations(info.Integrations)
}

// gatewayVersion renders the gateway's version and commit, e.g. "v1.2.3 (0a5e6ea2)".
func gatewayVersion(info *GatewayInfoResponse) string {
	return displayText(info.Version.Version) + " (" + displayText(info.Version.CommitHash) + ")"
}

func printIntegrations(integrations GatewayIntegrationsInfo) {
	var enabled []string
	if integrations.Slack {
		enabled = append(enabled, "Slack")
	}
	if integrations.GitHub {
		enabled = append(enabled, "GitHub")
	}
	if integrations.CircleCI {
		enabled = append(enabled, "CircleCI")
	}
	if len(enabled) > 0 {
		fmt.Println()
		fmt.Printf("Integrations:    %s\n", strings.Join(enabled, ", "))
	}
}
