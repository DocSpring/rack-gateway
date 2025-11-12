package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// PsCommand creates the ps command
func PsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "list app processes",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Ps(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("release", "", "filter by release")
	cmd.Flags().StringP("service", "s", "", "filter by service")
	cmd.Flags().Bool("all", false, "show all processes")

	// Add subcommands
	cmd.AddCommand(psInfoCommand())
	cmd.AddCommand(psStopCommand())

	return cmd
}

func psInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <id>",
		Short: "get information about a process",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps info")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.PsInfo(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

func psStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "stop a process",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "ps stop")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.PsStop(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

// ExecCommand creates the exec command
func ExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <pid> <command>",
		Short: "execute a command in a running process",
		Args:  cobra.MinimumNArgs(2),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupConvoxWithMFAAction(cobraCmd, args, "exec")
			if err != nil {
				return err
			}
			return normalizeConvoxExit(cli.Exec(client, ctx))
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("entrypoint", "", "override entrypoint")

	return cmd
}

// RunCommand creates the run command
func RunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [service] [command]",
		Short: "run a one-off process",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupConvoxWithMFAAction(cobraCmd, args, "run")
			if err != nil {
				return err
			}
			return normalizeConvoxExit(cli.Run(client, ctx))
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().BoolP("detach", "d", false, "run in detached mode")
	cmd.Flags().String("entrypoint", "", "override entrypoint")
	cmd.Flags().String("release", "", "release to run")
	cmd.Flags().StringSlice("env", []string{}, "environment variables")

	return cmd
}

// RestartCommand creates the restart command
func RestartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "restart an app",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "restart")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Restart(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

// ScaleCommand creates the scale command
func ScaleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scale",
		Short: "scale app processes",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "scale")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Scale(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Int("count", 0, "number of processes")
	cmd.Flags().Int("cpu", 0, "cpu allocation")
	cmd.Flags().Int("memory", 0, "memory allocation")

	return cmd
}
