package cli

import "github.com/spf13/cobra"

func DeployApprovalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-approval",
		Short: "Manage deploy approvals",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newDeployApprovalRequestCommand(),
		newDeployApprovalApproveCommand(),
		newDeployApprovalWaitCommand(),
	)

	return cmd
}
