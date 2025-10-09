package cli

import (
	"github.com/spf13/cobra"
)

// Execute runs the root command
func Execute() error {
	rootCmd := &cobra.Command{
		Use:           "rack-gateway",
		Short:         "API gateway for Convox with authentication and RBAC",
		SilenceErrors: true,
		Long: `Rack Gateway provides secure authenticated access to Convox racks
with SSO authentication, role-based access control, and audit logging.

To run convox commands through the gateway:
  rack-gateway ps
  rack-gateway apps
  rack-gateway deploy

Recommended aliases for your shell:
  alias cx="rack-gateway"   # cx apps, cx ps, cx deploy
  alias cg="rack-gateway"   # cg login, cg switch, cg rack

Rack management:
  rack-gateway rack                # Show current rack
  rack-gateway racks               # List all racks
  rack-gateway switch <rack>       # Switch to a different rack
  rack-gateway login <rack> <url>  # Login to a new rack`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no subcommand is specified, show help
			return cmd.Help()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&ConfigPath, "config", ConfigPath, "Config directory")
	rootCmd.PersistentFlags().StringVar(&RackFlag, "rack", "", "Rack to use (overrides current rack)")
	rootCmd.PersistentFlags().StringVar(&APITokenFlag, "api-token", "", "API token to use for CLI requests (overrides RACK_GATEWAY_API_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&MFAMethodFlag, "mfa-method", "", "MFA method to use (totp or webauthn)")
	rootCmd.PersistentFlags().StringVar(&MFACodeFlag, "mfa-code", "", "MFA code (TOTP/Yubikey/backup) for step-up authentication")

	// Add all commands
	rootCmd.AddCommand(
		// CLI commands
		LoginCommand(),
		LogoutCommand(),
		SwitchCommand(),
		RacksCommand(),
		VersionCommand(),
		WebCommand(),
		CompletionCommand(),

		// Gateway API commands
		APITokenCommand(),
		DeployApprovalCommand(),
		TestAuthCommand(),

		// Convox API commands (direct integration)
		// Apps
		AppsCommand(),

		// Builds and Deploys
		BuildCommand(),
		BuildsCommand(),
		DeployCommand(),
		ReleasesCommand(),

		// Processes
		PsCommand(),
		ExecCommand(),
		RunCommand(),
		RestartCommand(),
		ScaleCommand(),

		// Environment
		EnvCommand(),

		// Logs and System
		LogsCommand(),
		RackCommand(),
		InstancesCommand(),

		// Resources
		ResourcesCommand(),
		CertificatesCommand(),
		RegistriesCommand(),
	)

	return rootCmd.Execute()
}
