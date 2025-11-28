package cli

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deployApprovalShowOptions struct {
	racks  string
	app    string
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

If no ID is provided, searches for the latest approval request matching the current app and git branch.
Use --branch or --commit to search by specific criteria instead.

Examples:
  # Show by ID
  cx deploy-approval show abc123-def456-...

  # Show latest for current app and git branch
  cx deploy-approval show

  # Show latest for a specific app
  cx deploy-approval show --app myapp

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
	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
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

	// Resolve app name (auto-detect from .convox/app or directory)
	app, err := ResolveApp(opts.app)
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
	return showBySearch(cmd, racks, app, branch, commit, opts.output)
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

type rackResult struct {
	rack string
	req  *deployApprovalRequest
}

func showBySearch(cmd *cobra.Command, racks []string, app, branch, commit, output string) error {
	results := collectResultsFromRacks(cmd, racks, app, branch, commit)

	if len(results) == 0 {
		if branch != "" {
			return fmt.Errorf("no deploy approval request found for app %q branch %q", app, branch)
		}
		return fmt.Errorf("no deploy approval request found for app %q commit %q", app, commit)
	}

	if output == "json" {
		return outputResultsAsJSON(cmd, results)
	}
	return outputResultsAsText(results, len(racks) > 1)
}

func collectResultsFromRacks(
	cmd *cobra.Command, racks []string, app, branch, commit string,
) []rackResult {
	var results []rackResult
	for _, rack := range racks {
		for _, status := range []string{"pending", "approved"} {
			req, found := searchForRequestInRack(cmd, rack, app, branch, commit, status)
			if found {
				results = append(results, rackResult{rack: rack, req: req})
				break
			}
		}
	}
	return results
}

func outputResultsAsJSON(cmd *cobra.Command, results []rackResult) error {
	if len(results) == 1 {
		return printJSON(cmd, *results[0].req)
	}
	reqs := make([]deployApprovalRequest, len(results))
	for i, r := range results {
		reqs[i] = *r.req
	}
	return printJSON(cmd, reqs)
}

func outputResultsAsText(results []rackResult, showRack bool) error {
	for i, r := range results {
		if i > 0 {
			fmt.Println()
		}
		if err := printDeployApprovalDetails(r.req, r.rack, showRack); err != nil {
			return err
		}
	}
	return nil
}

func printDeployApprovalDetails(req *deployApprovalRequest, rack string, showRack bool) error {
	if showRack {
		fmt.Printf("Rack:     %s\n", rack)
	}
	fmt.Printf("ID:       %s\n", req.PublicID)
	fmt.Printf("Status:   %s\n", req.Status)
	fmt.Printf("Message:  %s\n", req.Message)
	if req.App != "" {
		fmt.Printf("App:      %s\n", req.App)
	}
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
