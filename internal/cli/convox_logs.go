package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// LogsCommand creates the logs command
func LogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "get logs for an app",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			if err := resolveAppFlag(cobraCmd); err != nil {
				return err
			}
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "logs")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Logs(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", appFlagHelp)
	cmd.Flags().String("filter", "", "filter logs")
	cmd.Flags().BoolP("follow", "f", false, "follow logs")
	cmd.Flags().String("since", "", "show logs since timestamp")

	return cmd
}

// InstancesCommand creates the instances command
func InstancesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "list instances",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "instances")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Instances(client, ctx)
		}),
	}

	return cmd
}
