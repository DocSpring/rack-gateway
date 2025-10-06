package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func LogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Logout from the current rack",
		RunE: func(cmd *cobra.Command, args []string) error {
			rack, err := SelectedRack()
			if err != nil {
				return err
			}

			removed, err := RemoveRack(rack)
			if err != nil {
				return fmt.Errorf("failed to remove rack: %w", err)
			}

			if !removed {
				fmt.Printf("Rack %s not found\n", rack)
				return nil
			}

			fmt.Printf("✓ Logged out from %s\n", rack)
			return nil
		},
	}
}
