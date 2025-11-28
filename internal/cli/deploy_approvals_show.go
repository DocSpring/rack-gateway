package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deployApprovalShowOptions struct {
	racks  string
	branch string
	commit string
	output string
}

func newDeployApprovalShowCommand() *cobra.Command {
	var opts deployApprovalShowOptions

	cmd := &cobra.Command{
		Use:   "show [id]",
		Short: "Show a deploy approval request",
		Long: `Show details for a deploy approval request.

If no ID is provided, searches for the latest approval request matching the current git branch.
Use --branch or --commit to search by specific criteria instead.

Examples:
  # Show by ID
  cx deploy-approval show abc123-def456-...

  # Show latest for current git branch
  cx deploy-approval show

  # Show latest for a specific branch
  cx deploy-approval show --branch main

  # Show for a specific commit
  cx deploy-approval show --commit abc123def

  # Show across multiple racks
  cx deploy-approval show --racks staging,us,eu`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeDeployApprovalShow(cmd, args, opts)
		},
	}

	cmd.Flags().StringVar(&opts.racks, "racks", "", "Comma-separated list of racks to search")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Search by git branch (uses current branch if no ID given)")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Search by git commit hash")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format (json)")

	return cmd
}

func executeDeployApprovalShow(cmd *cobra.Command, args []string, opts deployApprovalShowOptions) error {
	racks, err := resolveRacks(opts.racks)
	if err != nil {
		return err
	}

	// If an ID is provided, fetch directly by ID
	if len(args) == 1 {
		publicID := strings.TrimSpace(args[0])
		if publicID == "" {
			return fmt.Errorf("deploy approval request ID cannot be empty")
		}
		if _, err := uuid.Parse(publicID); err != nil {
			return fmt.Errorf("invalid deploy approval request ID format: must be a valid UUID")
		}
		return showByID(cmd, racks, publicID, opts.output)
	}

	// No ID provided - search by branch or commit
	branch, commit, err := resolveBranchOrCommit(opts.branch, opts.commit)
	if err != nil {
		return err
	}
	return showBySearch(cmd, racks, branch, commit, opts.output)
}

func showByID(cmd *cobra.Command, racks []string, publicID, output string) error {
	// Try each rack until we find the request
	var lastErr error
	for _, rack := range racks {
		endpoint := fmt.Sprintf("/deploy-approval-requests/%s", publicID)
		var result deployApprovalRequest
		if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
			lastErr = err
			continue
		}

		if output == "json" {
			return printJSON(cmd, result)
		}
		return printDeployApprovalDetails(&result, rack, len(racks) > 1)
	}

	if lastErr != nil {
		return fmt.Errorf("deploy approval request not found: %w", lastErr)
	}
	return fmt.Errorf("deploy approval request %s not found", publicID)
}

func showBySearch(cmd *cobra.Command, racks []string, branch, commit, output string) error {
	// Try pending first, then approved
	for _, status := range []string{"pending", "approved"} {
		req, rack, found := searchForRequest(cmd, racks, branch, commit, status)
		if found {
			if output == "json" {
				return printJSON(cmd, *req)
			}
			return printDeployApprovalDetails(req, rack, len(racks) > 1)
		}
	}

	if branch != "" {
		return fmt.Errorf("no deploy approval request found for branch %q", branch)
	}
	return fmt.Errorf("no deploy approval request found for commit %q", commit)
}

func searchForRequest(
	cmd *cobra.Command, racks []string, branch, commit, status string,
) (*deployApprovalRequest, string, bool) {
	params := url.Values{}
	params.Set("status", status)
	params.Set("limit", "1")
	if branch != "" {
		params.Set("git_branch", branch)
	}
	if commit != "" {
		params.Set("git_commit", commit)
	}
	endpoint := "/deploy-approval-requests?" + params.Encode()

	for _, rack := range racks {
		var result deployApprovalRequestList
		if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
			continue
		}
		if len(result.DeployApprovalRequests) > 0 {
			return &result.DeployApprovalRequests[0], rack, true
		}
	}
	return nil, "", false
}

func printDeployApprovalDetails(req *deployApprovalRequest, rack string, showRack bool) error {
	if showRack {
		fmt.Printf("Rack:     %s\n", rack)
	}
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
