package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// VersionCommand returns the version command
func VersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show rack-gateway CLI, gateway server, and rack versions",
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			// Always show CLI version
			fmt.Printf("client: %s\n", Version)

			// Try to get gateway and rack versions (requires login)
			rack, err := SelectedRack()
			if err != nil {
				fmt.Println("gateway: (not logged in)")
				fmt.Println("server: (not logged in)")
				return nil
			}

			gatewayURL, token, err := LoadRackAuth(rack)
			if err != nil {
				fmt.Println("gateway: (not logged in)")
				fmt.Println("server: (not logged in)")
				return nil
			}

			// Get gateway info
			info, err := GetGatewayInfo(gatewayURL, token)
			if err != nil {
				fmt.Printf("gateway: (error: %v)\n", err)
				fmt.Println("server: (unavailable)")
				return nil
			}

			// Show gateway version
			if info.Version.Version != "" {
				fmt.Printf("gateway: %s (%s)\n", info.Version.Version, info.Version.CommitHash)
			} else {
				fmt.Println("gateway: (unknown)")
			}

			// Get rack/server version via Convox SDK
			client, _, err := SetupConvoxCommand(cobraCmd, args)
			if err != nil {
				fmt.Println("server: (unavailable)")
				return nil
			}

			system, err := client.SystemGet()
			if err != nil {
				fmt.Println("server: (unavailable)")
				return nil
			}

			fmt.Printf("server: %s\n", system.Version)

			return nil
		}),
	}
}
