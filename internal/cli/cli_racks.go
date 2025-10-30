package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// RacksCommand returns the cobra command for listing configured racks.
func RacksCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "racks",
		Short: "List all configured racks",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Gateways) == 0 {
				fmt.Println("No racks configured. Use 'rack-gateway login' to add a rack.")
				return nil
			}

			fmt.Println("Configured racks:")
			for rack := range cfg.Gateways {
				marker := " "
				if rack == cfg.Current {
					marker = "*"
				}
				fmt.Printf("%s %s\n", marker, rack)
			}
			return nil
		},
	}
}
