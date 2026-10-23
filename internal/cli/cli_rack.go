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
		Short: "Show the selected rack's configuration",
		Long: "Display the selected rack's name, gateway URL, and login status from local configuration (no API " +
			"call). That's the current rack unless --rack or RACK_GATEWAY_RACK selects another.",
		Args: cobra.NoArgs,
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
	cmd.AddCommand(rackPsCommand())
	cmd.AddCommand(rackReleasesCommand())

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
