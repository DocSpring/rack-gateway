package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newDeployApprovalWaitCommand() *cobra.Command {
	var opts deployApprovalWaitOptions

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for and optionally approve pending deploy approval requests",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			parsed, err := parseDeployApprovalWaitOptions(cmd, opts)
			if err != nil {
				return err
			}
			return runDeployApprovalWait(cmd, parsed)
		}),
	}

	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Filter by git branch")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Filter by git commit hash (uses current commit by default)")
	cmd.Flags().StringVar(&opts.pollInterval, "poll-interval", "1s", "Polling interval")
	cmd.Flags().BoolVar(&opts.autoApprove, "approve", false, "Automatically approve the first pending request found")
	cmd.Flags().StringVar(&opts.notes, "notes", "", "Optional notes for approval (only used with --approve)")
	cmd.Flags().
		BoolVar(&opts.loop, "loop", false, "Continue polling for more requests after displaying or approving one")

	return cmd
}

type deployApprovalWaitOptions struct {
	app          string
	branch       string
	commit       string
	pollInterval string
	autoApprove  bool
	notes        string
	loop         bool
}

type deployApprovalWaitConfig struct {
	racks        []string
	app          string
	branch       string
	commit       string
	pollInterval time.Duration
	autoApprove  bool
	notes        string
	loop         bool
}

func parseDeployApprovalWaitOptions(
	_ *cobra.Command,
	opts deployApprovalWaitOptions,
) (deployApprovalWaitConfig, error) {
	racks, err := resolveRacks()
	if err != nil {
		return deployApprovalWaitConfig{}, err
	}

	app, err := ResolveApp(opts.app)
	if err != nil {
		return deployApprovalWaitConfig{}, err
	}

	branch, commit, err := resolveBranchOrCommit(opts.branch, opts.commit)
	if err != nil {
		return deployApprovalWaitConfig{}, err
	}

	pollInterval, err := parseDurationFlag(opts.pollInterval, "poll-interval", false, time.Second)
	if err != nil {
		return deployApprovalWaitConfig{}, err
	}

	return deployApprovalWaitConfig{
		racks:        racks,
		app:          app,
		branch:       branch,
		commit:       commit,
		pollInterval: pollInterval,
		autoApprove:  opts.autoApprove,
		notes:        strings.TrimSpace(opts.notes),
		loop:         opts.loop,
	}, nil
}

func runDeployApprovalWait(cmd *cobra.Command, cfg deployApprovalWaitConfig) error {
	waiter := newDeployApprovalWaiter(cmd, cfg)
	defer waiter.waitForSound()

	if err := waiter.printWaitingMessage(); err != nil {
		return err
	}

	for {
		done, err := waiter.pollNextRack()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		waiter.sleep()
	}
}

func newDeployApprovalWaiter(cmd *cobra.Command, cfg deployApprovalWaitConfig) *deployApprovalWaiter {
	return &deployApprovalWaiter{
		cmd:           cmd,
		racks:         cfg.racks,
		app:           cfg.app,
		branch:        cfg.branch,
		commit:        cfg.commit,
		pollInterval:  cfg.pollInterval,
		autoApprove:   cfg.autoApprove,
		notes:         cfg.notes,
		loop:          cfg.loop,
		approvedRacks: make(map[string]bool),
	}
}

// pollNextRack checks the next rack in rotation for pending/approved requests.
// Returns (true, nil) when all racks are done and we should exit.
func (w *deployApprovalWaiter) pollNextRack() (bool, error) {
	rack := w.nextRack()

	// Skip racks we've already processed
	if w.approvedRacks[rack] {
		return w.shouldExit(), nil
	}

	// Check for pending requests first
	if done, err := w.checkPendingRequests(rack); err != nil || done {
		return done, err
	}

	// If we just handled a request (marked rack as approved), we're done with this rack
	if w.approvedRacks[rack] {
		return w.shouldExit(), nil
	}

	// Check for already-approved requests (from previous sessions or other users)
	return w.checkApprovedRequests(rack)
}

func (w *deployApprovalWaiter) checkPendingRequests(rack string) (bool, error) {
	requests, err := fetchPendingDeployRequests(w.cmd, rack, w.app, w.branch, w.commit)
	if err != nil {
		return false, err
	}

	if len(requests) == 0 {
		return false, nil
	}

	if err := w.handleRequest(rack, requests[0]); err != nil {
		return false, err
	}
	return w.shouldExit(), nil
}

func (w *deployApprovalWaiter) checkApprovedRequests(rack string) (bool, error) {
	approved, err := fetchApprovedDeployRequests(w.cmd, rack, w.app, w.branch, w.commit)
	if err != nil {
		return false, err
	}

	if len(approved) > 0 {
		w.approvedRacks[rack] = true
		_ = writef(w.cmd.OutOrStdout(), "✓ Already approved on rack %s: %s\n", rack, approved[0].PublicID)
	}
	return w.shouldExit(), nil
}

type deployApprovalWaiter struct {
	cmd           *cobra.Command
	racks         []string
	app           string
	branch        string
	commit        string
	pollInterval  time.Duration
	autoApprove   bool
	notes         string
	loop          bool
	rackIndex     int
	cachedPIN     string
	approvedRacks map[string]bool
	soundDone     chan struct{}
}

func (w *deployApprovalWaiter) nextRack() string {
	if len(w.racks) == 0 {
		return ""
	}
	rack := w.racks[w.rackIndex]
	w.rackIndex = (w.rackIndex + 1) % len(w.racks)
	return rack
}

func (w *deployApprovalWaiter) shouldExit() bool {
	// In loop mode, never exit based on approval count
	if w.loop {
		return false
	}

	// Exit when all racks have approved requests
	if len(w.approvedRacks) >= len(w.racks) {
		return true
	}

	return false
}

func (w *deployApprovalWaiter) printWaitingMessage() error {
	if len(w.racks) == 0 {
		return nil
	}

	// Build filter description
	filter := fmt.Sprintf("app=%s", w.app)
	if w.branch != "" {
		filter += fmt.Sprintf(" branch=%s", w.branch)
	}
	if w.commit != "" {
		filter += fmt.Sprintf(" commit=%s", w.commit)
	}

	if len(w.racks) == 1 {
		return writef(
			w.cmd.OutOrStdout(),
			"Waiting for pending deploy approval requests on rack %s (%s)\n",
			w.racks[0], filter,
		)
	}
	return writef(
		w.cmd.OutOrStdout(),
		"Waiting for pending deploy approval requests on %d racks: %s (%s)\n",
		len(w.racks), strings.Join(w.racks, ", "), filter,
	)
}

func fetchDeployRequestsByStatus(
	cmd *cobra.Command, rack, app, branch, commit, status string,
) ([]deployApprovalRequest, error) {
	var response struct {
		Requests []deployApprovalRequest `json:"deploy_approval_requests"`
	}
	params := url.Values{}
	params.Set("status", status)
	params.Set("app", app)
	if branch != "" {
		params.Set("git_branch", branch)
	}
	if commit != "" {
		params.Set("git_commit", commit)
	}
	endpoint := "/deploy-approval-requests?" + params.Encode()
	if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	if response.Requests == nil {
		return nil, fmt.Errorf("unexpected API response format: missing 'deploy_approval_requests' field")
	}
	return response.Requests, nil
}

func fetchPendingDeployRequests(
	cmd *cobra.Command, rack, app, branch, commit string,
) ([]deployApprovalRequest, error) {
	return fetchDeployRequestsByStatus(cmd, rack, app, branch, commit, "pending")
}

func fetchApprovedDeployRequests(
	cmd *cobra.Command, rack, app, branch, commit string,
) ([]deployApprovalRequest, error) {
	return fetchDeployRequestsByStatus(cmd, rack, app, branch, commit, "approved")
}

func (w *deployApprovalWaiter) handleRequest(rack string, request deployApprovalRequest) error {
	// Mark this rack as having an approved request (pending counts too, we'll approve it)
	w.approvedRacks[rack] = true

	w.playNotificationOnce(rack)

	if err := w.writeRequestSummary(rack, request); err != nil {
		return err
	}

	if !w.autoApprove {
		msg := "\nUse 'rack-gateway deploy-approval approve <id>' to approve this request."
		return writeLine(w.cmd.OutOrStdout(), msg)
	}

	// Approve with PIN caching
	approved, pin, err := approveDeployRequestWithPIN(w.cmd, rack, request.PublicID, w.notes, w.cachedPIN)
	if err != nil {
		return err
	}

	// Cache the PIN for subsequent approvals
	if w.cachedPIN == "" && pin != "" {
		w.cachedPIN = pin
	}

	statusLine := fmt.Sprintf("\n✅ Deploy approval request %s approved", approved.PublicID)
	if len(w.racks) > 1 {
		statusLine = fmt.Sprintf("\n✅ Deploy approval request %s approved on rack %s", approved.PublicID, rack)
	}
	if approved.ApprovalExpiresAt != nil {
		statusLine = fmt.Sprintf(
			"%s (expires at %s)",
			statusLine,
			approved.ApprovalExpiresAt.UTC().Format(time.RFC3339),
		)
	}
	return writeLine(w.cmd.OutOrStdout(), statusLine)
}

// playNotificationOnce plays the notification sound a single time per invocation,
// in the background, so multiple rack approvals don't queue up extra bells or block
// the interactive approval flow. waitForSound (called on exit) lets it finish.
func (w *deployApprovalWaiter) playNotificationOnce(rack string) {
	if w.soundDone != nil {
		return
	}
	cfg, _, _ := LoadConfig()
	w.soundDone = make(chan struct{})
	go func() {
		defer close(w.soundDone)
		if err := playNotificationSound(cfg, rack); err != nil {
			_ = writef(w.cmd.OutOrStdout(), "Warning: failed to play notification sound: %v\n", err)
		}
	}()
}

func (w *deployApprovalWaiter) waitForSound() {
	if w.soundDone != nil {
		<-w.soundDone
	}
}

func (w *deployApprovalWaiter) writeRequestSummary(rack string, request deployApprovalRequest) error {
	out := w.cmd.OutOrStdout()
	if len(w.racks) > 1 {
		if err := writef(out, "\n📋 Deploy Approval Request Found on rack '%s':\n", rack); err != nil {
			return err
		}
	} else {
		if err := writeLine(out, "\n📋 Deploy Approval Request Found:"); err != nil {
			return err
		}
	}

	if err := writef(out, "  ID: %s\n", request.PublicID); err != nil {
		return err
	}
	if err := writef(out, "  Message: %s\n", request.Message); err != nil {
		return err
	}
	if err := writef(out, "  Status: %s\n", request.Status); err != nil {
		return err
	}
	if err := writef(out, "  Token: %s\n", request.TargetAPITokenName); err != nil {
		return err
	}
	return writef(out, "  Created: %s\n", request.CreatedAt.Format(time.RFC3339))
}

func (w *deployApprovalWaiter) sleep() {
	time.Sleep(w.pollInterval)
}
