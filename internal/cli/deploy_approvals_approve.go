package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newDeployApprovalApproveCommand() *cobra.Command {
	var (
		rackFlag string
		notes    string
	)

	cmd := &cobra.Command{
		Use:   "approve <request_id>",
		Short: "Approve a deploy approval request",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			requestID := strings.TrimSpace(args[0])
			if requestID == "" {
				return fmt.Errorf("request_id is required")
			}

			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			if trimmed := strings.TrimSpace(rackFlag); trimmed != "" {
				rack = trimmed
			}

			approved, err := approveDeployRequest(cmd, rack, requestID, strings.TrimSpace(notes))
			if err != nil {
				return err
			}

			statusLine := fmt.Sprintf("Deploy approval request %s approved", approved.PublicID)
			if approved.ApprovalExpiresAt != nil {
				statusLine = fmt.Sprintf("%s (expires at %s)", statusLine, approved.ApprovalExpiresAt.UTC().Format(time.RFC3339))
			}
			return writeLine(cmd.OutOrStdout(), statusLine)
		}),
	}

	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional notes for approval")

	return cmd
}

func approveDeployRequest(cmd *cobra.Command, rack, requestID, notes string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}

	endpoint := fmt.Sprintf("/deploy-approval-requests/%s/approve", requestID)
	return postDeployApprovalRequest(cmd, rack, endpoint, payload)
}
