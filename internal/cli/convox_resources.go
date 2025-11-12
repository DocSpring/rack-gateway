package cli

import (
	"github.com/convox/convox/pkg/cli"
	"github.com/spf13/cobra"
)

// ResourcesCommand creates the resources command
func ResourcesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "list resources",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "resources")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Resources(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	// Add subcommands
	cmd.AddCommand(resourcesInfoCommand())
	cmd.AddCommand(resourcesUrlCommand())
	cmd.AddCommand(resourcesExportCommand())

	return cmd
}

func resourcesExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "export a resource",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "resources export")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.ResourcesExport(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")
	cmd.Flags().StringP("file", "f", "", "output file")

	return cmd
}

func resourcesInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "get information about a resource",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "resources info")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.ResourcesInfo(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

func resourcesUrlCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "url <name>",
		Short: "get resource URL",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "resources url")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.ResourcesUrl(client, ctx)
		}),
	}

	cmd.Flags().StringP("app", "a", "", "app name")

	return cmd
}

// CertificatesCommand creates the certificates command
func CertificatesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "certs",
		Short: "list certificates",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "certs")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Certs(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(certsDeleteCommand())
	cmd.AddCommand(certsGenerateCommand())
	cmd.AddCommand(certsImportCommand())

	return cmd
}

func certsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "delete a certificate",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "certs delete")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.CertsDelete(client, ctx)
		}),
	}

	return cmd
}

func certsGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <domain>...",
		Short: "generate a certificate",
		Args:  cobra.MinimumNArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "certs generate")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.CertsGenerate(client, ctx)
		}),
	}

	return cmd
}

func certsImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <pub> <key>",
		Short: "import a certificate",
		Args:  cobra.ExactArgs(2),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "certs import")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.CertsImport(client, ctx)
		}),
	}

	cmd.Flags().String("chain", "", "certificate chain")

	return cmd
}

// RegistriesCommand creates the registries command
func RegistriesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registries",
		Short: "list registries",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "registries")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.Registries(client, ctx)
		}),
	}

	// Add subcommands
	cmd.AddCommand(registriesAddCommand())
	cmd.AddCommand(registriesRemoveCommand())

	return cmd
}

func registriesAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <server> <username> <password>",
		Short: "add a registry",
		Args:  cobra.ExactArgs(3),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "registries add")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.RegistriesAdd(client, ctx)
		}),
	}

	return cmd
}

func registriesRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <server>",
		Short: "remove a registry",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cobraCmd *cobra.Command, args []string) error {
			mfaAuth, err := checkMFAAndGetAuth(cobraCmd, "registries remove")
			if err != nil {
				return err
			}

			client, ctx, err := SetupConvoxCommandWithMFA(cobraCmd, args, mfaAuth)
			if err != nil {
				return err
			}
			return cli.RegistriesRemove(client, ctx)
		}),
	}

	return cmd
}
