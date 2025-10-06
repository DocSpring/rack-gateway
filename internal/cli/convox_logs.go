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
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "logs")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "filter", "follow", "since")
			if err != nil {
				return err
			}
			return cli.Logs(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("filter", "", "filter logs")
	cmd.Flags().BoolP("follow", "f", false, "follow logs")
	cmd.Flags().String("since", "", "show logs since timestamp")

	return cmd
}

// RackCommand creates the rack command
func RackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rack",
		Short: "get rack information",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Rack(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(rackLogsCommand())
	cmd.AddCommand(rackParamsCommand())
	cmd.AddCommand(rackPsCommand())
	cmd.AddCommand(rackReleasesCommand())
	cmd.AddCommand(rackScaleCommand())
	cmd.AddCommand(rackUpdateCommand())

	return cmd
}

func rackLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "get rack logs",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack logs")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "filter", "follow", "since")
			if err != nil {
				return err
			}
			return cli.RackLogs(client, ctx)
		}),
	}

	cmd.Flags().String("filter", "", "filter logs")
	cmd.Flags().BoolP("follow", "f", false, "follow logs")
	cmd.Flags().String("since", "", "show logs since timestamp")

	return cmd
}

func rackParamsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "display rack parameters",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack params")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.RackParams(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(rackParamsSetCommand())

	return cmd
}

func rackParamsSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key=value>...",
		Short: "set rack parameters",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack params set")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "wait")
			if err != nil {
				return err
			}
			return cli.RackParamsSet(client, ctx)
		}),
	}

	cmd.Flags().Bool("wait", false, "wait for update to complete")

	return cmd
}

func rackPsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "list rack processes",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack ps")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "all")
			if err != nil {
				return err
			}
			return cli.RackPs(client, ctx)
		}),
	}

	cmd.Flags().Bool("all", false, "show all processes")

	return cmd
}

func rackReleasesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "releases",
		Short: "list rack releases",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack releases")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.RackReleases(client, ctx)
		}),
	}

	return cmd
}

func rackScaleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scale",
		Short: "scale the rack",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack scale")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "count", "type")
			if err != nil {
				return err
			}
			return cli.RackScale(client, ctx)
		}),
	}

	cmd.Flags().Int("count", 0, "number of instances")
	cmd.Flags().String("type", "", "instance type")

	return cmd
}

func rackUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "update the rack",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack update")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "version", "wait")
			if err != nil {
				return err
			}
			return cli.RackUpdate(client, ctx)
		}),
	}

	cmd.Flags().String("version", "", "rack version")
	cmd.Flags().Bool("wait", false, "wait for update to complete")

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

	// Add subcommands
	cmd.AddCommand(instancesKeyrollCommand())
	cmd.AddCommand(instancesTerminateCommand())

	return cmd
}

func instancesKeyrollCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keyroll",
		Short: "roll SSH keys",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "instances keyroll")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "wait")
			if err != nil {
				return err
			}
			return cli.InstancesKeyroll(client, ctx)
		}),
	}

	cmd.Flags().Bool("wait", false, "wait for keyroll to complete")

	return cmd
}

func instancesTerminateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "terminate <id>",
		Short: "terminate an instance",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "instances terminate")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.InstancesTerminate(client, ctx)
		}),
	}

	return cmd
}
