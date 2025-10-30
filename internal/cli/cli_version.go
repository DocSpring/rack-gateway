package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// VersionCommand returns the version command
func VersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show rack-gateway version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("rack-gateway version %s (built %s)\n", Version, BuildTime)
		},
	}
}
