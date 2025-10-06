package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func SwitchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "switch <rack>",
		Short: "Switch to a different rack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rack := args[0]

			// Verify the rack exists
			cfg, _, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if _, ok := cfg.Gateways[rack]; !ok {
				return fmt.Errorf("rack %s not found. Use 'rack-gateway racks' to list available racks", rack)
			}

			if err := SetCurrentRack(rack); err != nil {
				return fmt.Errorf("failed to switch rack: %w", err)
			}

			fmt.Printf("✓ Switched to rack: %s\n", rack)
			return nil
		},
	}
}
