package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// errAPITokenCannotApprove explains why approving with RACK_GATEWAY_API_TOKEN fails: the gateway only
// accepts approvals from people, after MFA.
var errAPITokenCannotApprove = errors.New(
	"API tokens can't approve deploy requests; unset RACK_GATEWAY_API_TOKEN and approve as a logged-in user",
)

type deployApprovalApproveOptions struct {
	app     string
	allApps bool
	branch  string
	commit  string
	notes   string
}

func newDeployApprovalApproveCommand() *cobra.Command {
	var opts deployApprovalApproveOptions

	cmd := &cobra.Command{
		Use:   "approve [id]",
		Short: "Approve a deploy approval request",
		Long: `Approve a deploy approval request.

If no ID is provided, finds the requests for the commit (the current git commit by default) of every listed
app on every listed rack, shows them, and approves the pending ones: one PIN, then one key touch per approval.
Requests that are already approved or deployed count as done. If a listed app has no request on a listed rack,
nothing is approved and the command fails.

From a terminal it asks for Enter first. Without a terminal (scripts, AI agents) it doesn't, and it needs an
explicit --app list and a full 40-character --commit (no --branch or --all-apps): the PIN dialog shows only the
command line, so it must say exactly what is approved.

Examples:
  # Approve by ID
  cx deploy-approval approve abc123-def456-...

  # Approve latest for current git commit (prompts for MFA)
  cx deploy-approval approve

  # Approve latest for a specific branch
  cx deploy-approval approve --branch main

  # Approve for a specific commit
  cx deploy-approval approve --commit abc123def

  # Approve across multiple racks (one PIN entry, one touch per rack)
  cx deploy-approval approve --rack staging,us,eu

  # Approve two apps' requests for a commit on US and EU (one PIN, one touch per approval)
  cx deploy-approval approve --app docspring,api-proxy --commit <sha> --rack us,eu

  # Approve every app's request for a commit
  cx deploy-approval approve --all-apps --commit <sha> --rack staging`,
		Args: cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			return executeDeployApprovalApprove(cmd, args, opts)
		}),
	}

	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appsFlagHelp)
	cmd.Flags().BoolVar(&opts.allApps, "all-apps", false, allAppsFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Search by git branch")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Search by git commit hash (uses current commit by default)")
	cmd.Flags().StringVar(&opts.notes, "notes", "", "Optional notes for approval")

	return cmd
}

func executeDeployApprovalApprove(cmd *cobra.Command, args []string, opts deployApprovalApproveOptions) error {
	racks, err := resolveRacks()
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
	err = requireExplicitApprovalTarget(IsInteractive(), opts.app, opts.allApps, opts.branch, opts.commit)
	if err != nil {
		return err
	}
	apps, err := resolveApprovalApps(opts.app, opts.allApps)
	if err != nil {
		return err
	}
	branch, commit, err := resolveBranchOrCommit(opts.branch, opts.commit)
	if err != nil {
		return err
	}
	return approveBySearch(cmd, racks, apps, branch, commit, opts.notes)
}

func approveByID(cmd *cobra.Command, racks []string, publicID, notes string) error {
	// Try each rack until we find and approve the request
	for _, rack := range racks {
		approved, err := approveDeployRequest(cmd, rack, publicID, notes)
		if err != nil {
			if isGatewayStatus(err, http.StatusNotFound) {
				continue
			}
			return rackScopedError(rack, err, len(racks))
		}

		if approved == nil {
			continue
		}

		return printApprovalSuccess(cmd, approved, rack, len(racks) > 1)
	}

	return fmt.Errorf("deploy approval request %s not found", publicID)
}

type rackApproval struct {
	rack string
	req  *deployApprovalRequest
}

func approveBySearch(cmd *cobra.Command, racks []string, apps approvalApps, branch, commit, notes string) error {
	allRequests, missing, err := collectAllRequests(cmd, racks, apps, branch, commit)
	if err != nil {
		return err
	}

	if len(missing) > 0 || len(allRequests) == 0 {
		return noRequestFoundError(missing, apps, branch, commit)
	}

	if err := displayAllRequests(allRequests, len(racks) > 1); err != nil {
		return err
	}

	pending := filterPendingRequests(allRequests)
	if len(pending) == 0 {
		fmt.Println("\nNothing to approve: every request is already approved or deployed.")
		return nil
	}

	// From a terminal, confirm first. Agents and scripts can't press Enter; the PIN dialog (which shows this
	// command) and the key touch per approval are the confirmation.
	if IsInteractive() {
		if err := promptForApproval(pending); err != nil {
			return err
		}
	}

	return approveAllRequests(cmd, pending, notes, len(racks) > 1)
}

// noRequestFoundError explains which expected requests are missing. Nothing is approved in that case, so a
// retry after the missing requests appear approves them all together.
func noRequestFoundError(missing []string, apps approvalApps, branch, commit string) error {
	target := "commit " + commit
	if branch != "" {
		target = "branch " + branch
	}
	if len(missing) == 0 {
		return fmt.Errorf("no deploy approval request found for %s (%s)", target, apps)
	}
	return fmt.Errorf(
		"no deploy approval request found for %s on %s; nothing was approved", target, strings.Join(missing, ", "),
	)
}

func displayAllRequests(requests []rackApproval, showRack bool) error {
	fmt.Println()
	for i, r := range requests {
		if i > 0 {
			fmt.Println()
		}
		if err := printDeployApprovalDetails(r.req, r.rack, showRack); err != nil {
			return err
		}
	}
	return nil
}

func filterPendingRequests(requests []rackApproval) []rackApproval {
	var pending []rackApproval
	for _, r := range requests {
		if r.req.Status == "pending" {
			pending = append(pending, r)
		}
	}
	return pending
}

func promptForApproval(pending []rackApproval) error {
	promptText := buildApprovalPrompt(pending)
	fmt.Print(promptText + " (or Ctrl+C to abort): ")

	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("aborted")
	}
	return nil
}

func buildApprovalPrompt(pending []rackApproval) string {
	if len(pending) == 1 {
		return fmt.Sprintf("\nPress Enter to approve %s on rack %s", displayText(pending[0].req.App), pending[0].rack)
	}
	targets := make([]string, len(pending))
	for i, p := range pending {
		targets[i] = p.rack + "/" + displayText(p.req.App)
	}
	return fmt.Sprintf("\nPress Enter to approve %d requests: %s", len(pending), strings.Join(targets, ", "))
}

// collectAllRequests finds, on each rack, the newest request of each in-scope app that is pending, approved or
// already deployed (in that order of preference). missing lists "rack/app" for listed apps with none (with
// --all-apps: racks with none).
func collectAllRequests(
	cmd *cobra.Command, racks []string, apps approvalApps, branch, commit string,
) ([]rackApproval, []string, error) {
	var results []rackApproval
	var missing []string
	for _, rack := range racks {
		found, err := collectRackRequests(cmd, rack, apps, branch, commit)
		if err != nil {
			return nil, nil, rackScopedError(rack, err, len(racks))
		}
		results = append(results, found...)
		missing = append(missing, missingApps(rack, apps, found)...)
	}
	return results, missing, nil
}

func collectRackRequests(
	cmd *cobra.Command, rack string, apps approvalApps, branch, commit string,
) ([]rackApproval, error) {
	var results []rackApproval
	found := map[string]bool{}
	for _, status := range []string{"pending", "approved", "deployed"} {
		requests, err := fetchDeployRequestsByStatus(cmd, rack, apps.queryApp(), branch, commit, status)
		if err != nil {
			return nil, err
		}
		for _, req := range apps.newestRequestPerApp(requests) {
			if !found[req.App] {
				found[req.App] = true
				results = append(results, rackApproval{rack: rack, req: &req})
			}
		}
	}
	return results, nil
}

func missingApps(rack string, apps approvalApps, found []rackApproval) []string {
	if apps.all {
		if len(found) == 0 {
			return []string{rack}
		}
		return nil
	}
	present := map[string]bool{}
	for _, r := range found {
		present[r.req.App] = true
	}
	var missing []string
	for _, app := range apps.names {
		if !present[app] {
			missing = append(missing, rack+"/"+app)
		}
	}
	return missing
}

func approveAllRequests(cmd *cobra.Command, pending []rackApproval, notes string, showRack bool) error {
	var cachedPIN string
	var successCount int

	for i, p := range pending {
		printApprovalContext(cmd, p, i+1, len(pending))

		approved, pin, err := approveDeployRequestWithPIN(cmd, p.rack, p.req.PublicID, notes, cachedPIN)
		if err != nil {
			return fmt.Errorf("failed to approve request on rack %s: %w", p.rack, err)
		}

		// Cache the PIN for subsequent approvals
		if cachedPIN == "" && pin != "" {
			cachedPIN = pin
		}

		if err := printApprovalSuccess(cmd, approved, p.rack, showRack); err != nil {
			return err
		}
		successCount++
	}

	if successCount > 1 {
		fmt.Printf("\n✅ Successfully approved %d requests\n", successCount)
	}

	return nil
}

func printApprovalContext(cmd *cobra.Command, p rackApproval, current, total int) {
	out := cmd.ErrOrStderr()
	_, _ = fmt.Fprintln(out)

	if total > 1 {
		_, _ = fmt.Fprintf(out, "Approving request %d of %d:\n", current, total)
	} else {
		_, _ = fmt.Fprintln(out, "Approving request:")
	}

	_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Rack:   "), p.rack)
	_, _ = fmt.Fprintf(out, "  %s %s\n", dim("ID:     "), p.req.PublicID)
	_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Message:"), displayText(p.req.Message))
	if p.req.App != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("App:    "), displayText(p.req.App))
	}
	if p.req.GitCommitHash != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Commit: "), p.req.GitCommitHash)
	}
	if p.req.GitBranch != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Branch: "), displayText(p.req.GitBranch))
	}
}

func approveDeployRequest(cmd *cobra.Command, rack, requestID, notes string) (*deployApprovalRequest, error) {
	result, _, err := approveDeployRequestWithPIN(cmd, rack, requestID, notes, "")
	return result, err
}

func approveDeployRequestWithPIN(
	cmd *cobra.Command, rack, requestID, notes, cachedPIN string,
) (*deployApprovalRequest, string, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}

	endpoint := fmt.Sprintf("/deploy-approval-requests/%s/approve", requestID)

	// Get MFA auth with PIN caching
	mfaAuth, pinUsed, err := getDeployApprovalMFAAuth(cmd, rack, cachedPIN)
	if err != nil {
		return nil, "", err
	}

	result, err := postDeployApprovalRequestWithMFA(cmd, rack, endpoint, payload, mfaAuth)
	if err != nil {
		return nil, "", err
	}

	return result, pinUsed, nil
}

func getDeployApprovalMFAAuth(cmd *cobra.Command, rack, cachedPIN string) (string, string, error) {
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		return "", "", errAPITokenCannotApprove
	}

	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", "", err
	}

	return GetMFAAuthWithPIN(cmd, gatewayURL, bearer, rack, cachedPIN)
}

func postDeployApprovalRequestWithMFA(
	cmd *cobra.Command, rack, endpoint string, payload map[string]interface{}, mfaAuth string,
) (*deployApprovalRequest, error) {
	var result deployApprovalRequest
	if err := gatewayRequestWithMFA(cmd, rack, "POST", endpoint, payload, &result, mfaAuth); err != nil {
		return nil, err
	}
	return &result, nil
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
