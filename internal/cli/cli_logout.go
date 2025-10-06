package cli

import "github.com/spf13/cobra"

func LogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "logout command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil // TODO
		},
	}
}
