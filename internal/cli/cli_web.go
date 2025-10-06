package cli

import "github.com/spf13/cobra"

func WebCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "web command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil // TODO
		},
	}
}
