package cli

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newDeployApprovalGetCommand() *cobra.Command {
	var rackFlag string
	var output string

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a deploy approval request by ID",
		Long:  "Get details for a specific deploy approval request by its public ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rack, err := resolveRackFlag(rackFlag)
			if err != nil {
				return err
			}

			publicID := strings.TrimSpace(args[0])
			if publicID == "" {
				return fmt.Errorf("deploy approval request ID is required")
			}

			// Validate UUID format to prevent path traversal attacks
			if _, err := uuid.Parse(publicID); err != nil {
				return fmt.Errorf("invalid deploy approval request ID format: must be a valid UUID")
			}

			endpoint := fmt.Sprintf("/deploy-approval-requests/%s", publicID)

			var result deployApprovalRequest
			if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
				return err
			}

			if output == "json" {
				return printJSON(cmd, result)
			}

			return printDeployApprovalDetails(&result)
		},
	}

	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format (json)")

	return cmd
}

func printDeployApprovalDetails(req *deployApprovalRequest) error {
	fmt.Printf("ID:       %s\n", req.PublicID)
	fmt.Printf("Status:   %s\n", req.Status)
	fmt.Printf("Message:  %s\n", req.Message)
	fmt.Printf("Created:  %s\n", req.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:  %s\n", req.UpdatedAt.Format(time.RFC3339))

	if req.TargetAPITokenName != "" {
		fmt.Printf("Token:    %s (%s)\n", req.TargetAPITokenName, req.TargetAPITokenID)
	} else {
		fmt.Printf("Token ID: %s\n", req.TargetAPITokenID)
	}

	if req.GitCommitHash != "" {
		fmt.Printf("Commit:   %s\n", req.GitCommitHash)
	}
	if req.GitBranch != "" {
		fmt.Printf("Branch:   %s\n", req.GitBranch)
	}

	if req.ApprovedAt != nil {
		fmt.Printf("Approved: %s\n", req.ApprovedAt.Format(time.RFC3339))
	}
	if req.ApprovalExpiresAt != nil {
		fmt.Printf("Expires:  %s\n", req.ApprovalExpiresAt.Format(time.RFC3339))
	}
	if req.RejectedAt != nil {
		fmt.Printf("Rejected: %s\n", req.RejectedAt.Format(time.RFC3339))
	}
	if req.ApprovalNotes != "" {
		fmt.Printf("Notes:    %s\n", req.ApprovalNotes)
	}

	return nil
}
