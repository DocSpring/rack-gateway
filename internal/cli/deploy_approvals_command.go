package cli

import "github.com/spf13/cobra"

// DeployApprovalCommand returns the cobra command for managing deploy approvals.
func DeployApprovalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-approval",
		Short: "Manage deploy approvals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newDeployApprovalListCommand(),
		newDeployApprovalShowCommand(),
		newDeployApprovalRequestCommand(),
		newDeployApprovalApproveCommand(),
		newDeployApprovalWaitCommand(),
	)

	return cmd
}
