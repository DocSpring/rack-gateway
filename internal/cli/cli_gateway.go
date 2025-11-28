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

func printGatewayInfo(gatewayURL string, info *GatewayInfoResponse) {
	// Gateway info
	fmt.Printf("Gateway URL:     %s\n", gatewayURL)
	if info.Version.Version != "" {
		fmt.Printf("Gateway Version: %s (%s)\n", info.Version.Version, info.Version.CommitHash)
	}

	// Rack info
	fmt.Println()
	fmt.Printf("Rack Name:       %s\n", info.Rack.Name)
	if info.Rack.Alias != "" && info.Rack.Alias != info.Rack.Name {
		fmt.Printf("Rack Alias:      %s\n", info.Rack.Alias)
	}
	if info.Rack.Host != "" {
		fmt.Printf("Rack Host:       %s\n", info.Rack.Host)
	}

	// User info
	fmt.Println()
	fmt.Printf("Logged in as:    %s\n", info.User.Email)
	if info.User.Name != "" {
		fmt.Printf("Name:            %s\n", info.User.Name)
	}
	if len(info.User.Roles) > 0 {
		fmt.Printf("Roles:           %s\n", strings.Join(info.User.Roles, ", "))
	}

	// Integrations
	printIntegrations(info.Integrations)
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
