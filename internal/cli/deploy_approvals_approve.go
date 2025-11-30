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

If no ID is provided, searches for pending approval requests matching the current git commit.
Shows all matching requests and prompts once before approving all of them.

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
  cx deploy-approval approve --racks staging,us,eu`,
		Args: cobra.MaximumNArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			return executeDeployApprovalApprove(cmd, args, opts)
		}),
	}

	cmd.Flags().StringVar(&opts.racks, "racks", "", "Comma-separated list of racks to search")
	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Search by git branch")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Search by git commit hash (uses current commit by default)")
	cmd.Flags().StringVar(&opts.notes, "notes", "", "Optional notes for approval")

	return cmd
}

func executeDeployApprovalApprove(cmd *cobra.Command, args []string, opts deployApprovalApproveOptions) error {
	racks, err := resolveRacks(opts.racks)
	if err != nil {
		return err
	}

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

type rackApproval struct {
	rack string
	req  *deployApprovalRequest
}

func approveBySearch(cmd *cobra.Command, racks []string, app, branch, commit, notes string) error {
	allRequests := collectAllRequests(cmd, racks, app, branch, commit)

	if len(allRequests) == 0 {
		return noRequestFoundError(app, branch, commit)
	}

	if err := displayAllRequests(allRequests, len(racks) > 1); err != nil {
		return err
	}

	pending := filterPendingRequests(allRequests)
	if len(pending) == 0 {
		fmt.Println("\nAll requests are already approved.")
		return nil
	}

	if err := promptForApproval(pending); err != nil {
		return err
	}

	return approveAllRequests(cmd, pending, notes, len(racks) > 1)
}

func noRequestFoundError(app, branch, commit string) error {
	if branch != "" {
		return fmt.Errorf("no deploy approval request found for app %q branch %q", app, branch)
	}
	return fmt.Errorf("no deploy approval request found for app %q commit %q", app, commit)
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
		return fmt.Sprintf("\nPress Enter to approve on rack %s", pending[0].rack)
	}
	rackNames := make([]string, len(pending))
	for i, p := range pending {
		rackNames[i] = p.rack
	}
	return fmt.Sprintf("\nPress Enter to approve %d requests on racks: %s",
		len(pending), strings.Join(rackNames, ", "))
}

func collectAllRequests(
	cmd *cobra.Command, racks []string, app, branch, commit string,
) []rackApproval {
	var results []rackApproval
	for _, rack := range racks {
		// Try pending first, then approved (like show command)
		for _, status := range []string{"pending", "approved"} {
			req, found := searchForRequestInRack(cmd, rack, app, branch, commit, status)
			if found {
				results = append(results, rackApproval{rack: rack, req: req})
				break
			}
		}
	}
	return results
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
	_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Message:"), p.req.Message)
	if p.req.App != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("App:    "), p.req.App)
	}
	if p.req.GitCommitHash != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Commit: "), p.req.GitCommitHash)
	}
	if p.req.GitBranch != "" {
		_, _ = fmt.Fprintf(out, "  %s %s\n", dim("Branch: "), p.req.GitBranch)
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
	mfaAuth, pinUsed, err := getMFAAuthWithPIN(cmd, rack, cachedPIN)
	if err != nil {
		return nil, "", err
	}

	result, err := postDeployApprovalRequestWithMFA(cmd, rack, endpoint, payload, mfaAuth)
	if err != nil {
		return nil, "", err
	}

	return result, pinUsed, nil
}

func getMFAAuthWithPIN(cmd *cobra.Command, rack, cachedPIN string) (string, string, error) {
	if os.Getenv("RACK_GATEWAY_API_TOKEN") != "" {
		return "", "", nil
	}

	gatewayURL, bearer, err := gatewayAuthInfo(rack)
	if err != nil {
		return "", "", err
	}

	status, err := loadMFAStatus(gatewayURL, bearer)
	if err != nil {
		return "", "", err
	}

	method, err := selectMFAMethod(status, rack)
	if err != nil {
		return "", "", err
	}

	return collectMFAAuthWithPIN(cmd, gatewayURL, bearer, method, cachedPIN)
}

func collectMFAAuthWithPIN(
	cmd *cobra.Command, baseURL, bearer string, method MFAMethodResponse, cachedPIN string,
) (string, string, error) {
	out := cmd.ErrOrStderr()

	switch method.Type {
	case "webauthn":
		// Only print the message if this is the first call (no cached PIN)
		if cachedPIN == "" {
			if err := writeLine(out, "Multi-factor authentication required (WebAuthn)."); err != nil {
				return "", "", err
			}
		}

		assertionData, pinUsed, err := collectWebAuthnAssertionWithPIN(baseURL, bearer, cachedPIN)
		if err != nil {
			return "", "", fmt.Errorf("WebAuthn verification failed: %w", err)
		}

		return "webauthn." + assertionData, pinUsed, nil

	case "totp":
		if err := writeLine(out, "Multi-factor authentication required (TOTP)."); err != nil {
			return "", "", err
		}

		code, err := promptMFACode()
		if err != nil {
			return "", "", err
		}

		return "totp." + code, "", nil

	default:
		return "", "", fmt.Errorf("unsupported MFA method for inline verification: %s", method.Type)
	}
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
