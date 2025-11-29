package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
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

	cmd.Flags().
		StringVar(&opts.racks, "racks", "", "Comma-separated list of rack names to monitor (e.g., dev,staging,prod)")
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
	racks        string
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
	racks, err := resolveRacks(opts.racks)
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
	waiter := &deployApprovalWaiter{
		cmd:          cmd,
		racks:        cfg.racks,
		app:          cfg.app,
		branch:       cfg.branch,
		commit:       cfg.commit,
		pollInterval: cfg.pollInterval,
		autoApprove:  cfg.autoApprove,
		notes:        cfg.notes,
		loop:         cfg.loop,
	}

	if err := waiter.printWaitingMessage(); err != nil {
		return err
	}

	for {
		rack := waiter.nextRack()
		requests, err := fetchPendingDeployRequests(cmd, rack, cfg.app, cfg.branch, cfg.commit)
		if err != nil {
			return err
		}

		if len(requests) > 0 {
			if err := waiter.handleRequest(rack, requests[0]); err != nil {
				return err
			}
			if !cfg.loop {
				return nil
			}
			if err := waiter.printWaitingMessage(); err != nil {
				return err
			}
			waiter.sleep()
			continue
		}

		waiter.sleep()
	}
}

type deployApprovalWaiter struct {
	cmd          *cobra.Command
	racks        []string
	app          string
	branch       string
	commit       string
	pollInterval time.Duration
	autoApprove  bool
	notes        string
	loop         bool
	rackIndex    int
}

func (w *deployApprovalWaiter) nextRack() string {
	if len(w.racks) == 0 {
		return ""
	}
	rack := w.racks[w.rackIndex]
	w.rackIndex = (w.rackIndex + 1) % len(w.racks)
	return rack
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

func fetchPendingDeployRequests(
	cmd *cobra.Command, rack, app, branch, commit string,
) ([]deployApprovalRequest, error) {
	var response struct {
		Requests []deployApprovalRequest `json:"deploy_approval_requests"`
	}
	params := url.Values{}
	params.Set("status", "pending")
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

func (w *deployApprovalWaiter) handleRequest(rack string, request deployApprovalRequest) error {
	cfg, _, _ := LoadConfig()
	soundDone := w.startNotificationSound(cfg, rack)

	if err := w.writeRequestSummary(rack, request); err != nil {
		<-soundDone
		return err
	}

	var err error
	if w.autoApprove {
		err = w.autoApproveRequest(rack, request)
	} else {
		msg := "\nUse 'rack-gateway deploy-approval approve <id>' to approve this request."
		err = writeLine(w.cmd.OutOrStdout(), msg)
	}

	<-soundDone
	return err
}

func (w *deployApprovalWaiter) startNotificationSound(cfg *Config, rack string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := playNotificationSound(cfg, rack); err != nil {
			_ = writef(w.cmd.OutOrStdout(), "Warning: failed to play notification sound: %v\n", err)
		}
	}()
	return done
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

func (w *deployApprovalWaiter) autoApproveRequest(rack string, request deployApprovalRequest) error {
	approved, err := approveDeployRequest(w.cmd, rack, request.PublicID, w.notes)
	if err != nil {
		return err
	}

	statusLine := fmt.Sprintf("\n✅ Deploy approval request %s approved", approved.PublicID)
	if approved.ApprovalExpiresAt != nil {
		statusLine = fmt.Sprintf(
			"%s (expires at %s)",
			statusLine,
			approved.ApprovalExpiresAt.UTC().Format(time.RFC3339),
		)
	}
	return writeLine(w.cmd.OutOrStdout(), statusLine)
}

func (w *deployApprovalWaiter) sleep() {
	time.Sleep(w.pollInterval)
}

func playNotificationSound(cfg *Config, rack string) error {
	preference := resolveNotificationPreference(cfg, rack)
	if preference == "disabled" {
		return nil
	}

	soundFile, cleanup, err := ensureSoundFile(preference)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	player, err := selectAudioPlayer(soundFile)
	if err != nil {
		return err
	}
	return player.Run()
}

func resolveNotificationPreference(cfg *Config, rack string) string {
	const defaultPreference = "default"
	if cfg == nil {
		return defaultPreference
	}
	preference := cfg.NotificationSound
	if preference == "" {
		preference = defaultPreference
	}
	if rack == "" || cfg.Gateways == nil {
		return preference
	}
	if gwCfg, ok := cfg.Gateways[rack]; ok && gwCfg.NotificationSound != "" {
		return gwCfg.NotificationSound
	}
	return preference
}

func ensureSoundFile(preference string) (string, func(), error) {
	if preference == "" || preference == "default" {
		return createTemporarySoundFile()
	}
	if _, err := os.Stat(preference); err != nil {
		return "", nil, fmt.Errorf("notification sound file not found: %w", err)
	}
	return preference, nil, nil
}

func createTemporarySoundFile() (string, func(), error) {
	tmpFile, err := os.CreateTemp("", "notification-*.mp3")
	if err != nil {
		return "", nil, err
	}

	if _, err := tmpFile.Write(notificationSound); err != nil {
		_ = tmpFile.Close()
		return "", nil, err
	}
	if err := tmpFile.Close(); err != nil {
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(tmpFile.Name())
	}
	return tmpFile.Name(), cleanup, nil
}

func selectAudioPlayer(soundFile string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		//nolint:gosec // G204: Command hardcoded, file path controlled
		return exec.Command("afplay", soundFile), nil
	case "linux":
		return linuxAudioPlayer(soundFile)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func linuxAudioPlayer(soundFile string) (*exec.Cmd, error) {
	for _, candidate := range []string{"paplay", "aplay", "ffplay", "mpg123"} {
		if _, err := exec.LookPath(candidate); err == nil {
			if candidate == "ffplay" {
				//nolint:gosec // G204: Command hardcoded, file path controlled
				return exec.Command(candidate, "-nodisp", "-autoexit", soundFile), nil
			}
			//nolint:gosec // G204: Command hardcoded, file path controlled
			return exec.Command(candidate, soundFile), nil
		}
	}
	return nil, fmt.Errorf("no audio player found (tried paplay, aplay, ffplay, mpg123)")
}
