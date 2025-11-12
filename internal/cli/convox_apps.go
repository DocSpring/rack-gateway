package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// AppsCommand creates the apps command with subcommands
func AppsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "manage apps",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			// Check MFA and get auth string
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Apps(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(appsCreateCommand())
	cmd.AddCommand(appsDeleteCommand())
	cmd.AddCommand(appsInfoCommand())
	cmd.AddCommand(appsCancelCommand())
	cmd.AddCommand(appsLockCommand())
	cmd.AddCommand(appsUnlockCommand())
	cmd.AddCommand(appsParamsCommand())

	return cmd
}

func appsCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "create an app",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps create")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsCreate(client, ctx)
		}),
	}

	cmd.Flags().String("generation", "", "generation")
	cmd.Flags().Bool("lock", false, "enable termination protection")

	return cmd
}

func appsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "delete an app",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps delete")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsDelete(client, ctx)
		}),
	}

	return cmd
}

func appsInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [app]",
		Short: "get information about an app",
		Args:  cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps info")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsInfo(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

func appsCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <name>",
		Short: "cancel an app update",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps cancel")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsCancel(client, ctx)
		}),
	}

	return cmd
}

func appsLockCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <name>",
		Short: "enable termination protection",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps lock")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsLock(client, ctx)
		}),
	}

	return cmd
}

func appsUnlockCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <name>",
		Short: "disable termination protection",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps unlock")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsUnlock(client, ctx)
		}),
	}

	return cmd
}

func appsParamsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "manage app parameters",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps params")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsParams(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(appsParamsSetCommand())

	return cmd
}

func appsParamsSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key=value>...",
		Short: "set app parameters",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "apps params set")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.AppsParamsSet(client, ctx)
		}),
	}

	return cmd
}
