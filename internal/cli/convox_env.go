package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// EnvCommand creates the env command
func EnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "manage environment variables",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Env(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	// Add subcommands
	cmd.AddCommand(envEditCommand())
	cmd.AddCommand(envGetCommand())
	cmd.AddCommand(envSetCommand())
	cmd.AddCommand(envUnsetCommand())

	return cmd
}

func envEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "edit environment interactively",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env edit")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "promote", "replace")
			if err != nil {
				return err
			}
			return cli.EnvEdit(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Bool("promote", false, "promote release after setting")
	cmd.Flags().StringSlice("replace", []string{}, "replace environment variables")

	return cmd
}

func envGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "get an environment variable",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env get")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.EnvGet(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

func envSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key=value>...",
		Short: "set environment variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env set")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "id", "promote", "replace")
			if err != nil {
				return err
			}
			return cli.EnvSet(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("id", "", "release id")
	cmd.Flags().Bool("promote", false, "promote release after setting")
	cmd.Flags().StringSlice("replace", []string{}, "replace environment variables")

	return cmd
}

func envUnsetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>...",
		Short: "unset environment variables",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "env unset")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "id", "promote")
			if err != nil {
				return err
			}
			return cli.EnvUnset(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("id", "", "release id")
	cmd.Flags().Bool("promote", false, "promote release after unsetting")

	return cmd
}
