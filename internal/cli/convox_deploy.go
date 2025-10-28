package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// DeployCommand creates the deploy command
func DeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [dir]",
		Short: "deploy an app",
		Args:  cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupConvoxWithMFAAction(cobraCmd, args, "deploy", "app", "description", "file", "manifest", "no-cache", "replace", "wait")
			if err != nil {
				return err
			}
			return cli.Deploy(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().String("description", "", "build description")
	cmd.Flags().StringP("file", "f", "", "path to Dockerfile")
	cmd.Flags().StringP("manifest", "m", "", "path to manifest file")
	cmd.Flags().Bool("no-cache", false, "disable build cache")
	cmd.Flags().StringSlice("replace", []string{}, "replace environment variable")
	cmd.Flags().Bool("wait", false, "wait for deployment to complete")

	return cmd
}

// ReleasesCommand creates the releases command
func ReleasesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "releases",
		Short: "list releases",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			client, ctx, err := setupConvoxWithMFAAction(cobraCmd, args, "releases", "limit")
			if err != nil {
				return err
			}
			return cli.Releases(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().IntP("limit", "l", 0, "limit number of releases")

	// Add subcommands
	cmd.AddCommand(releasesInfoCommand())
	cmd.AddCommand(releasesPromoteCommand())
	cmd.AddCommand(releasesRollbackCommand())

	return cmd
}

func releasesInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <id>",
		Short: "get information about a release",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "releases info")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.ReleasesInfo(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

func releasesPromoteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "promote a release",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "releases promote")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "wait", "force")
			if err != nil {
				return err
			}
			return cli.ReleasesPromote(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Bool("wait", false, "wait for promotion to complete")
	cmd.Flags().Bool("force", false, "force promotion")

	return cmd
}

func releasesRollbackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <id>",
		Short: "roll back to a previous release",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "releases rollback")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth, "wait")
			if err != nil {
				return err
			}
			return cli.ReleasesRollback(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().Bool("wait", false, "wait for rollback to complete")

	return cmd
}
