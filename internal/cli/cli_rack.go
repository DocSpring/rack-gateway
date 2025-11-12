package cli

import (
	"fmt"
	"time"

	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// RackCommand creates the rack command (shows local config + subcommands for remote operations)
func RackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rack",
		Short: "Show current rack configuration",
		Long:  "Display the current rack name, gateway URL, and login status from local configuration (no API call)",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(_ *cobra.Command, _ []string) error {
			status, err := ResolveRackStatus(time.Now())
			if err != nil {
				return err
			}

			fmt.Printf("Current rack: %s\n", status.Rack)
			fmt.Printf("Gateway URL: %s\n", status.GatewayURL)
			for _, line := range status.StatusLines {
				fmt.Println(line)
			}

			return nil
		}),
	}

	// Add subcommands for remote rack operations
	cmd.AddCommand(rackInfoCommand())
	cmd.AddCommand(rackLogsCommand())
	cmd.AddCommand(rackParamsCommand())
	cmd.AddCommand(rackPsCommand())
	cmd.AddCommand(rackReleasesCommand())
	cmd.AddCommand(rackScaleCommand())
	cmd.AddCommand(rackUpdateCommand())

	return cmd
}

func rackInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Get remote rack information",
		Long:  "Query the Convox API for rack details (provider, region, status, etc.)",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "rack info")
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
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
