package cli

import "github.com/spf13/cobra"

func LoginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login [rack] [gateway-url]",
		Short: "Login to a Convox rack via OAuth",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil // TODO
		},
	}
}
