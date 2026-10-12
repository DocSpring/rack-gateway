package cli

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deployApprovalShowOptions struct {
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
  cx deploy-approval show --rack staging,us,eu`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeDeployApprovalShow(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Search by git branch (uses current branch if no ID given)")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Search by git commit hash")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Output format (json)")

	return cmd
}

func executeDeployApprovalShow(cmd *cobra.Command, args []string, opts deployApprovalShowOptions) error {
	racks, err := resolveRacks()
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
	found, err := findRequestByID(cmd, racks, publicID)
	if err != nil {
		return err
	}
	if output == "json" {
		return printJSON(cmd, *found.req)
	}
	return printDeployApprovalDetails(cmd.OutOrStdout(), found.req, found.rack, len(racks) > 1)
}

// findRequestByID looks the request up on each rack in turn and returns the first match.
func findRequestByID(cmd *cobra.Command, racks []string, publicID string) (rackApproval, error) {
	endpoint := fmt.Sprintf("/deploy-approval-requests/%s", publicID)
	for _, rack := range racks {
		var result deployApprovalRequest
		if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
			if isGatewayStatus(err, http.StatusNotFound) {
				continue
			}
			return rackApproval{}, rackScopedError(rack, err, len(racks))
		}
		if result.PublicID != "" {
			return rackApproval{rack: rack, req: &result}, nil
		}
	}
	return rackApproval{}, fmt.Errorf("deploy approval request %s not found", publicID)
}

func showBySearch(cmd *cobra.Command, racks []string, app, branch, commit, output string) error {
	results, err := collectResultsFromRacks(cmd, racks, app, branch, commit)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		if branch != "" {
			return fmt.Errorf("no deploy approval request found for app %q branch %q", app, branch)
		}
		return fmt.Errorf("no deploy approval request found for app %q commit %q", app, commit)
	}

	if output == "json" {
		return outputResultsAsJSON(cmd, results)
	}
	return outputResultsAsText(cmd.OutOrStdout(), results, len(racks) > 1)
}

func collectResultsFromRacks(
	cmd *cobra.Command, racks []string, app, branch, commit string,
) ([]rackApproval, error) {
	var results []rackApproval
	for _, rack := range racks {
		for _, status := range []string{"pending", "approved"} {
			req, found, err := searchForRequestInRack(cmd, rack, app, branch, commit, status)
			if err != nil {
				return nil, rackScopedError(rack, err, len(racks))
			}
			if found {
				results = append(results, rackApproval{rack: rack, req: req})
				break
			}
		}
	}
	return results, nil
}

func outputResultsAsJSON(cmd *cobra.Command, results []rackApproval) error {
	if len(results) == 1 {
		return printJSON(cmd, *results[0].req)
	}
	reqs := make([]deployApprovalRequest, len(results))
	for i, r := range results {
		reqs[i] = *r.req
	}
	return printJSON(cmd, reqs)
}

func outputResultsAsText(out io.Writer, results []rackApproval, showRack bool) error {
	for i, r := range results {
		if i > 0 {
			if err := writeLine(out); err != nil {
				return err
			}
		}
		if err := printDeployApprovalDetails(out, r.req, r.rack, showRack); err != nil {
			return err
		}
	}
	return nil
}

// printDeployApprovalDetails prints every field of a request, one per line. Every field comes from the gateway
// (and much of it from CI: message, app, branch, commit, token name, notes), so each goes through displayText.
func printDeployApprovalDetails(out io.Writer, req *deployApprovalRequest, rack string, showRack bool) error {
	var details strings.Builder
	if showRack {
		writeDetail(&details, "Rack:    ", rack)
	}
	writeDetail(&details, "ID:      ", displayText(req.PublicID))
	writeDetail(&details, "Status:  ", statusColor(req.Status))
	writeDetail(&details, "Message: ", displayText(req.Message))
	if req.App != "" {
		writeDetail(&details, "App:     ", displayText(req.App))
	}
	writeDetail(&details, "Created: ", req.CreatedAt.Format(time.RFC3339))
	writeDetail(&details, "Updated: ", req.UpdatedAt.Format(time.RFC3339))

	if req.TargetAPITokenName != "" {
		tokenID := dim("(" + displayText(req.TargetAPITokenID) + ")")
		writeDetail(&details, "Token:   ", displayText(req.TargetAPITokenName)+" "+tokenID)
	} else {
		writeDetail(&details, "Token ID:", displayText(req.TargetAPITokenID))
	}

	if req.GitCommitHash != "" {
		writeDetail(&details, "Commit:  ", displayText(req.GitCommitHash))
	}
	if req.GitBranch != "" {
		writeDetail(&details, "Branch:  ", displayText(req.GitBranch))
	}
	if req.PrURL != "" {
		writeDetail(&details, "PR:      ", displayText(req.PrURL))
	}

	writeTimeDetail(&details, "Approved:", req.ApprovedAt)
	writeTimeDetail(&details, "Expires: ", req.ApprovalExpiresAt)
	writeTimeDetail(&details, "Rejected:", req.RejectedAt)
	if req.ApprovalNotes != "" {
		writeDetail(&details, "Notes:   ", displayText(req.ApprovalNotes))
	}

	_, err := io.WriteString(out, details.String())
	return err
}

func writeDetail(details *strings.Builder, label, value string) {
	details.WriteString(dim(label) + " " + value + "\n")
}

func writeTimeDetail(details *strings.Builder, label string, value *time.Time) {
	if value != nil {
		writeDetail(details, label, value.Format(time.RFC3339))
	}
}
