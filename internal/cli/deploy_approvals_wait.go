package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newDeployApprovalWaitCommand() *cobra.Command {
	var opts deployApprovalWaitOptions

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for and optionally approve pending deploy approval requests",
		Long: `Wait for deploy approval requests for a commit and optionally approve them.

Without --loop, waits until every listed app has an approved (or deployed) request on every rack, then prints
what was approved. Without a terminal (e.g. an AI agent), --approve needs an explicit --app list and a full
--commit SHA.

Examples:
  # Approve two apps' requests for a commit on staging (one PIN, one touch per approval)
  rack-gateway deploy-approval wait --approve --app docspring,api-proxy --commit <sha> --rack staging`,
		Args: cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			parsed, err := parseDeployApprovalWaitOptions(cmd, opts)
			if err != nil {
				return err
			}
			return runDeployApprovalWait(cmd, parsed)
		}),
	}

	cmd.Flags().StringVarP(&opts.app, "app", "a", "", appsFlagHelp)
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Filter by git branch")
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Filter by git commit hash (uses current commit by default)")
	cmd.Flags().StringVar(&opts.pollInterval, "poll-interval", "1s", "Polling interval")
	cmd.Flags().BoolVar(&opts.autoApprove, "approve", false, "Approve each matching pending request as it appears")
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
	apps         approvalApps
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

	if opts.autoApprove {
		err := requireExplicitApprovalTarget(IsInteractive(), opts.app, false, opts.branch, opts.commit)
		if err != nil {
			return deployApprovalWaitConfig{}, err
		}
	}

	apps, err := resolveApprovalApps(opts.app, false)
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
		apps:         apps,
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
			return waiter.printSummary()
		}
		waiter.sleep()
	}
}

func newDeployApprovalWaiter(cmd *cobra.Command, cfg deployApprovalWaitConfig) *deployApprovalWaiter {
	return &deployApprovalWaiter{
		cmd:          cmd,
		racks:        cfg.racks,
		apps:         cfg.apps,
		branch:       cfg.branch,
		commit:       cfg.commit,
		pollInterval: cfg.pollInterval,
		autoApprove:  cfg.autoApprove,
		notes:        cfg.notes,
		loop:         cfg.loop,
		handled:      make(map[string]map[string]string),
	}
}

// pollNextRack checks the next rack in rotation: it handles every in-scope pending request (approving it with
// --approve) and records requests that are already approved. Returns (true, nil) when we should exit.
func (w *deployApprovalWaiter) pollNextRack() (bool, error) {
	rack := w.nextRack()
	if w.rackDone(rack) {
		return w.shouldExit(), nil
	}

	pending, err := w.fetch(rack, "pending")
	if err != nil {
		return false, err
	}
	for _, request := range pending {
		if w.isHandled(rack, request.App) {
			continue
		}
		if err := w.handleRequest(rack, request); err != nil {
			return false, err
		}
	}

	// Approved (or already deployed) requests count as done.
	for _, status := range []string{"approved", "deployed"} {
		requests, err := w.fetch(rack, status)
		if err != nil {
			return false, err
		}
		for _, request := range requests {
			if w.isHandled(rack, request.App) {
				continue
			}
			w.markHandled(rack, request)
			_ = writef(w.cmd.OutOrStdout(), "✓ Already %s on rack %s: %s (%s)\n",
				status, rack, request.PublicID, displayText(request.App))
		}
	}
	return w.shouldExit(), nil
}

// fetch returns the newest in-scope request of each app on rack with the given status.
func (w *deployApprovalWaiter) fetch(rack, status string) ([]deployApprovalRequest, error) {
	requests, err := fetchDeployRequestsByStatus(w.cmd, rack, w.apps.queryApp(), w.branch, w.commit, status)
	if err != nil {
		return nil, rackScopedError(rack, err, len(w.racks))
	}
	return w.apps.newestRequestPerApp(requests), nil
}

func (w *deployApprovalWaiter) isHandled(rack, app string) bool {
	_, ok := w.handled[rack][app]
	return ok
}

func (w *deployApprovalWaiter) markHandled(rack string, request deployApprovalRequest) {
	if w.handled[rack] == nil {
		w.handled[rack] = make(map[string]string)
	}
	w.handled[rack][request.App] = request.PublicID
}

// rackDone reports whether every expected app on rack has been handled.
func (w *deployApprovalWaiter) rackDone(rack string) bool {
	for _, app := range w.apps.names {
		if !w.isHandled(rack, app) {
			return false
		}
	}
	return true
}

type deployApprovalWaiter struct {
	cmd          *cobra.Command
	racks        []string
	apps         approvalApps
	branch       string
	commit       string
	pollInterval time.Duration
	autoApprove  bool
	notes        string
	loop         bool
	rackIndex    int
	cachedPIN    string
	// handled maps rack -> app -> request ID for requests approved (or shown) by this run or already approved.
	handled   map[string]map[string]string
	soundDone chan struct{}
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

	for _, rack := range w.racks {
		if !w.rackDone(rack) {
			return false
		}
	}
	return true
}

// printSummary lists the requests this run approved or found approved, per rack and app.
func (w *deployApprovalWaiter) printSummary() error {
	if !w.autoApprove {
		return nil
	}
	out := w.cmd.OutOrStdout()
	if err := writeLine(out, "\nApproved:"); err != nil {
		return err
	}
	for _, rack := range w.racks {
		apps := make([]string, 0, len(w.handled[rack]))
		for app := range w.handled[rack] {
			apps = append(apps, app)
		}
		sort.Strings(apps)
		for _, app := range apps {
			if err := writef(out, "  %s  %s  %s\n", rack, app, w.handled[rack][app]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *deployApprovalWaiter) printWaitingMessage() error {
	if len(w.racks) == 0 {
		return nil
	}

	// Build filter description
	filter := fmt.Sprintf("apps=%s", w.apps)
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
	if app != "" {
		params.Set("app", app)
	}
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

func (w *deployApprovalWaiter) handleRequest(rack string, request deployApprovalRequest) error {
	// Pending counts as handled too: we approve it now, or show how to approve it.
	w.markHandled(rack, request)

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

	app := displayText(request.App)
	statusLine := fmt.Sprintf("\n✅ Deploy approval request %s (%s) approved", approved.PublicID, app)
	if len(w.racks) > 1 {
		statusLine = fmt.Sprintf(
			"\n✅ Deploy approval request %s (%s) approved on rack %s", approved.PublicID, app, rack,
		)
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
	if err := writef(out, "  App: %s\n", displayText(request.App)); err != nil {
		return err
	}
	if err := writef(out, "  Message: %s\n", displayText(request.Message)); err != nil {
		return err
	}
	if err := writef(out, "  Status: %s\n", request.Status); err != nil {
		return err
	}
	if err := writef(out, "  Token: %s\n", displayText(request.TargetAPITokenName)); err != nil {
		return err
	}
	return writef(out, "  Created: %s\n", request.CreatedAt.Format(time.RFC3339))
}

func (w *deployApprovalWaiter) sleep() {
	time.Sleep(w.pollInterval)
}
