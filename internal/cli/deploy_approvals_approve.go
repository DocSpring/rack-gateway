package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deployApprovalApproveOptions struct {
	racks  string
	app    string
	branch string
	commit string
	notes  string
}

func newDeployApprovalApproveCommand() *cobra.Command {
	var opts deployApprovalApproveOptions

	cmd := &cobra.Command{
		Use:   "approve [id]",
		Short: "Approve a deploy approval request",
		Long: `Approve a deploy approval request.

If no ID is provided, searches for the latest pending approval request matching the current git branch.
Shows the request details and prompts for MFA code before approving.

Examples:
  # Approve by ID
  cx deploy-approval approve abc123-def456-...

  # Approve latest for current git branch (prompts for MFA)
  cx deploy-approval approve

  # Approve latest for a specific branch
  cx deploy-approval approve --branch main

  # Approve for a specific commit
  cx deploy-approval approve --commit abc123def

  # Approve across multiple racks
  cx deploy-approval approve --racks staging,us,eu`,
		Args: cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			return executeDeployApprovalApprove(cmd, args, opts)
		}),
	}

	cmd.Flags().StringVar(&opts.racks, "racks", "", "Comma-separated list of racks to search")
	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Search by git branch (uses current branch if no ID given)")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Search by git commit hash")
	cmd.Flags().StringVar(&opts.notes, "notes", "", "Optional notes for approval")

	return cmd
}

func executeDeployApprovalApprove(cmd *cobra.Command, args []string, opts deployApprovalApproveOptions) error {
	racks, err := resolveRacks(opts.racks)
	if err != nil {
		return err
	}

	// Resolve app name (auto-detect from .convox/app or directory)
	app, err := ResolveApp(opts.app)
	if err != nil {
		return err
	}

	// If an ID is provided, approve directly by ID
	if len(args) == 1 {
		publicID := strings.TrimSpace(args[0])
		if publicID == "" {
			return fmt.Errorf("deploy approval request ID cannot be empty")
		}
		if _, err := uuid.Parse(publicID); err != nil {
			return fmt.Errorf("invalid deploy approval request ID format: must be a valid UUID")
		}
		return approveByID(cmd, racks, publicID, opts.notes)
	}

	// No ID provided - search by branch or commit
	branch, commit, err := resolveBranchOrCommit(opts.branch, opts.commit)
	if err != nil {
		return err
	}
	return approveBySearch(cmd, racks, app, branch, commit, opts.notes)
}

func approveByID(cmd *cobra.Command, racks []string, publicID, notes string) error {
	// Try each rack until we find and approve the request
	var lastErr error
	for _, rack := range racks {
		approved, err := approveDeployRequest(cmd, rack, publicID, notes)
		if err != nil {
			lastErr = err
			continue
		}

		return printApprovalSuccess(cmd, approved, rack, len(racks) > 1)
	}

	if lastErr != nil {
		return fmt.Errorf("failed to approve request: %w", lastErr)
	}
	return fmt.Errorf("deploy approval request %s not found", publicID)
}

func approveBySearch(cmd *cobra.Command, racks []string, app, branch, commit, notes string) error {
	// Search each rack for a pending request
	for _, rack := range racks {
		req := findPendingRequest(cmd, rack, app, branch, commit)
		if req == nil {
			continue
		}

		// Found a request - show details and prompt for confirmation
		showRack := len(racks) > 1
		fmt.Println("\n📋 Deploy Approval Request Found:")
		if err := printDeployApprovalDetails(req, rack, showRack); err != nil {
			return err
		}

		fmt.Print("\nPress Enter to approve (or Ctrl+C to abort): ")
		reader := bufio.NewReader(os.Stdin)
		if _, err := reader.ReadString('\n'); err != nil {
			return fmt.Errorf("aborted")
		}

		approved, err := approveDeployRequest(cmd, rack, req.PublicID, notes)
		if err != nil {
			return err
		}

		return printApprovalSuccess(cmd, approved, rack, showRack)
	}

	if branch != "" {
		return fmt.Errorf("no pending deploy approval request found for app %q branch %q", app, branch)
	}
	return fmt.Errorf("no pending deploy approval request found for app %q commit %q", app, commit)
}

func findPendingRequest(cmd *cobra.Command, rack, app, branch, commit string) *deployApprovalRequest {
	req, found := searchForRequestInRack(cmd, rack, app, branch, commit, "pending")
	if !found {
		return nil
	}
	return req
}

func approveDeployRequest(cmd *cobra.Command, rack, requestID, notes string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}

	endpoint := fmt.Sprintf("/deploy-approval-requests/%s/approve", requestID)
	return postDeployApprovalRequest(cmd, rack, endpoint, payload)
}

func printApprovalSuccess(cmd *cobra.Command, approved *deployApprovalRequest, rack string, showRack bool) error {
	var statusLine string
	if showRack {
		statusLine = fmt.Sprintf("\n✅ Deploy approval request %s approved on rack %s", approved.PublicID, rack)
	} else {
		statusLine = fmt.Sprintf("\n✅ Deploy approval request %s approved", approved.PublicID)
	}
	if approved.ApprovalExpiresAt != nil {
		statusLine = fmt.Sprintf(
			"%s (expires at %s)",
			statusLine,
			approved.ApprovalExpiresAt.UTC().Format(time.RFC3339),
		)
	}
	return writeLine(cmd.OutOrStdout(), statusLine)
}
