package cli

import "github.com/spf13/cobra"

func CompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion",
		Short: "completion command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil // TODO
		},
	}
}
