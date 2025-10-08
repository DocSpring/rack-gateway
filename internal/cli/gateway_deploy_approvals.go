package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

//go:embed assets/notification.mp3
var notificationSound []byte

type deployApprovalRequest struct {
	ID                 int64                  `json:"id"`
	Message            string                 `json:"message"`
	Status             string                 `json:"status"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	TargetAPITokenID   string                 `json:"target_api_token_id"`
	TargetAPITokenName string                 `json:"target_api_token_name,omitempty"`
	ApprovedAt         *time.Time             `json:"approved_at,omitempty"`
	ApprovalExpiresAt  *time.Time             `json:"approval_expires_at,omitempty"`
	RejectedAt         *time.Time             `json:"rejected_at,omitempty"`
	ApprovalNotes      string                 `json:"approval_notes,omitempty"`
	GitCommitHash      string                 `json:"git_commit_hash"`
	GitBranch          string                 `json:"git_branch,omitempty"`
	PipelineURL        string                 `json:"pipeline_url,omitempty"`
	CIProvider         string                 `json:"ci_provider,omitempty"`
	CIMetadata         map[string]interface{} `json:"ci_metadata,omitempty"`
}

type deployApprovalRequestConflictError struct {
	request *deployApprovalRequest
}

func (e *deployApprovalRequestConflictError) Error() string {
	return "deploy approval request already exists"
}

func DeployApprovalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-approval",
		Short: "Manage deploy approvals",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newDeployApprovalRequestCommand(), newDeployApprovalApproveCommand(), newDeployApprovalWaitCommand())

	return cmd
}

func newDeployApprovalRequestCommand() *cobra.Command {
	var (
		rackFlag        string
		waitFlag        bool
		pollIntervalStr string
		timeoutStr      string
		gitCommitHash   string
		gitBranch       string
		pipelineURL     string
		ciProvider      string
		message         string
	)

	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request manual approval for CI/CD deploy",
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			// Validate required flags
			gitCommitHash = strings.TrimSpace(gitCommitHash)
			if gitCommitHash == "" {
				return fmt.Errorf("--git-commit is required")
			}

			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("--message is required")
			}

			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			if strings.TrimSpace(rackFlag) != "" {
				rack = strings.TrimSpace(rackFlag)
			}

			pollInterval := 5 * time.Second
			if strings.TrimSpace(pollIntervalStr) != "" {
				dur, err := time.ParseDuration(pollIntervalStr)
				if err != nil {
					return fmt.Errorf("invalid --poll-interval: %w", err)
				}
				if dur <= 0 {
					return fmt.Errorf("--poll-interval must be positive")
				}
				pollInterval = dur
			}

			timeout := 0 * time.Second
			if strings.TrimSpace(timeoutStr) != "" {
				dur, err := time.ParseDuration(timeoutStr)
				if err != nil {
					return fmt.Errorf("invalid --timeout: %w", err)
				}
				timeout = dur
			}

			gatewayURL, bearer, err := gatewayAuthInfo(rack)
			if err != nil {
				return err
			}

			created, err := createDeployApproval(cmd, gatewayURL, bearer, rack, gitCommitHash, gitBranch, pipelineURL, ciProvider, message, "", nil)
			if err != nil {
				var conflict *deployApprovalRequestConflictError
				if errors.As(err, &conflict) && conflict.request != nil {
					created = conflict.request
					if err := writef(cmd.OutOrStdout(), "Deploy approval request %d already exists (status: %s)\n", created.ID, created.Status); err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				if err := writef(cmd.OutOrStdout(), "Deploy approval request %d created (status: %s)\n", created.ID, created.Status); err != nil {
					return err
				}
			}

			if created == nil {
				return fmt.Errorf("failed to create deploy approval request")
			}

			if waitFlag {
				final, err := waitForDeployApproval(cmd, gatewayURL, bearer, rack, created.ID, pollInterval, timeout)
				if err != nil {
					return err
				}
				switch strings.ToLower(final.Status) {
				case "approved", "expired":
					if err := writef(cmd.OutOrStdout(), "Deploy approval request %d approved.\n", final.ID); err != nil {
						return err
					}
					return nil
				case "rejected":
					note := strings.TrimSpace(final.ApprovalNotes)
					if note != "" {
						return fmt.Errorf("deploy approval request %d rejected: %s", final.ID, note)
					}
					return fmt.Errorf("deploy approval request %d rejected", final.ID)
				default:
					return fmt.Errorf("deploy approval request %d finished with status: %s", final.ID, final.Status)
				}
			}

			return nil
		}),
	}

	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name")
	cmd.Flags().BoolVar(&waitFlag, "wait", false, "Block until approval is decided")
	cmd.Flags().StringVar(&pollIntervalStr, "poll-interval", "5s", "Polling interval when --wait is set")
	cmd.Flags().StringVar(
		&timeoutStr,
		"timeout",
		"20m",
		"Maximum time to wait before giving up (set to 0 to wait indefinitely)",
	)
	cmd.Flags().StringVar(&gitCommitHash, "git-commit", "", "Git commit SHA (required)")
	cmd.Flags().StringVar(&gitBranch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&pipelineURL, "pipeline-url", "", "CI pipeline URL (e.g., CircleCI build URL)")
	cmd.Flags().StringVar(&ciProvider, "ci-provider", "", "CI provider (circleci, github, buildkite, jenkins, etc.)")
	cmd.Flags().StringVar(&message, "message", "", "Deploy approval message (required)")

	_ = cmd.MarkFlagRequired("git-commit")
	_ = cmd.MarkFlagRequired("message")

	return cmd
}

func newDeployApprovalApproveCommand() *cobra.Command {
	var (
		rackFlag string
		mfaCode  string
		notes    string
	)

	cmd := &cobra.Command{
		Use:   "approve <request_id>",
		Short: "Approve a deploy approval request",
		Args:  cobra.ExactArgs(1),
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			requestID := strings.TrimSpace(args[0])
			if requestID == "" {
				return fmt.Errorf("request_id is required")
			}

			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			if strings.TrimSpace(rackFlag) != "" {
				rack = strings.TrimSpace(rackFlag)
			}

			gatewayURL, bearer, err := gatewayAuthInfo(rack)
			if err != nil {
				return err
			}

			approved, err := approveDeployRequest(cmd, gatewayURL, bearer, rack, requestID, strings.TrimSpace(notes), &mfaCode)
			if err != nil {
				return err
			}

			statusLine := fmt.Sprintf("Deploy approval request %d approved", approved.ID)
			if approved.ApprovalExpiresAt != nil {
				statusLine = fmt.Sprintf("%s (expires at %s)", statusLine, approved.ApprovalExpiresAt.UTC().Format(time.RFC3339))
			}
			if err := writeLine(cmd.OutOrStdout(), statusLine); err != nil {
				return err
			}
			return nil
		}),
	}

	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name")
	cmd.Flags().StringVar(&mfaCode, "mfa-code", "", "MFA code to satisfy step-up requirements")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional notes for approval")

	return cmd
}

func newDeployApprovalWaitCommand() *cobra.Command {
	var (
		racksFlag       string
		pollIntervalStr string
		mfaCode         string
		autoApprove     bool
		notes           string
		loop            bool
	)

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for and optionally approve pending deploy approval requests",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, args []string) error {
			// Parse racks list
			var racks []string
			if strings.TrimSpace(racksFlag) != "" {
				for _, r := range strings.Split(racksFlag, ",") {
					if r = strings.TrimSpace(r); r != "" {
						racks = append(racks, r)
					}
				}
			} else {
				// Default to current rack
				rack, err := SelectedRack()
				if err != nil {
					return err
				}
				racks = []string{rack}
			}

			if len(racks) == 0 {
				return fmt.Errorf("no racks specified")
			}

			pollInterval := 1 * time.Second
			if strings.TrimSpace(pollIntervalStr) != "" {
				dur, err := time.ParseDuration(pollIntervalStr)
				if err != nil {
					return fmt.Errorf("invalid --poll-interval: %w", err)
				}
				if dur <= 0 {
					return fmt.Errorf("--poll-interval must be positive")
				}
				pollInterval = dur
			}

			// Prepare auth info for all racks
			type rackInfo struct {
				name       string
				gatewayURL string
				bearer     string
			}
			rackInfos := make([]rackInfo, 0, len(racks))
			for _, rack := range racks {
				gatewayURL, bearer, err := gatewayAuthInfo(rack)
				if err != nil {
					return fmt.Errorf("failed to get auth info for rack %s: %w", rack, err)
				}
				rackInfos = append(rackInfos, rackInfo{
					name:       rack,
					gatewayURL: gatewayURL,
					bearer:     bearer,
				})
			}

			rackIndex := 0
			printWaitingMessage := func() error {
				if len(racks) == 1 {
					return writef(cmd.OutOrStdout(), "Waiting for pending deploy approval requests on rack: %s\n", racks[0])
				}
				return writef(cmd.OutOrStdout(), "Waiting for pending deploy approval requests on %d racks: %s\n", len(racks), strings.Join(racks, ", "))
			}

			// Print initial waiting message
			if err := printWaitingMessage(); err != nil {
				return err
			}

			for {
				// Round-robin through racks
				info := rackInfos[rackIndex]
				rackIndex = (rackIndex + 1) % len(rackInfos)
				// List pending deploy approval requests
				resp, body, err := sendDeployApprovalRequest(info.gatewayURL, info.bearer, http.MethodGet, "/admin/deploy-approval-requests?status=pending", nil)
				if err != nil {
					return err
				}

				if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
					if err := satisfyMFAStepUp(cmd, info.gatewayURL, info.bearer, info.name, &mfaCode); err != nil {
						return err
					}
					resp, body, err = sendDeployApprovalRequest(info.gatewayURL, info.bearer, http.MethodGet, "/admin/deploy-approval-requests?status=pending", nil)
					if err != nil {
						return err
					}
				}

				if resp.StatusCode >= 400 {
					return fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
				}

				var result struct {
					Requests []deployApprovalRequest `json:"deploy_approval_requests"`
				}
				if err := json.Unmarshal(body, &result); err != nil {
					return fmt.Errorf("failed to parse deploy approval requests response: %w", err)
				}

				// Verify the response structure - if Requests is nil (not just empty), the JSON field wasn't present
				if result.Requests == nil {
					return fmt.Errorf("unexpected API response format: missing 'deploy_approval_requests' field")
				}

				if len(result.Requests) > 0 {
					// Found a pending request! Play sound asynchronously and show details
					cfg, _, _ := LoadConfig()
					soundDone := make(chan struct{})
					go func() {
						defer close(soundDone)
						if err := playNotificationSound(cfg, info.name); err != nil {
							// Don't fail the command if sound playback fails
							_ = writef(cmd.OutOrStdout(), "Warning: failed to play notification sound: %v\n", err)
						}
					}()

					req := result.Requests[0]
					if len(rackInfos) > 1 {
						if err := writef(cmd.OutOrStdout(), "\n📋 Deploy Approval Request Found on rack '%s':\n", info.name); err != nil {
							return err
						}
					} else {
						if err := writeLine(cmd.OutOrStdout(), "\n📋 Deploy Approval Request Found:"); err != nil {
							return err
						}
					}
					if err := writef(cmd.OutOrStdout(), "  ID: %d\n", req.ID); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Message: %s\n", req.Message); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Status: %s\n", req.Status); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Token: %s\n", req.TargetAPITokenName); err != nil {
						return err
					}
					if err := writef(cmd.OutOrStdout(), "  Created: %s\n", req.CreatedAt.Format(time.RFC3339)); err != nil {
						return err
					}

					if autoApprove {
						if err := writeLine(cmd.OutOrStdout(), "\n🔐 Multi-factor authentication required to approve."); err != nil {
							return err
						}

						// Perform MFA step-up before approval
						if err := satisfyMFAStepUp(cmd, info.gatewayURL, info.bearer, info.name, &mfaCode); err != nil {
							return err
						}

						// Now approve the request
						approved, err := approveDeployRequest(cmd, info.gatewayURL, info.bearer, info.name, fmt.Sprintf("%d", req.ID), strings.TrimSpace(notes), &mfaCode)
						if err != nil {
							return err
						}

						statusLine := fmt.Sprintf("\n✅ Deploy approval request %d approved", approved.ID)
						if approved.ApprovalExpiresAt != nil {
							statusLine = fmt.Sprintf("%s (expires at %s)", statusLine, approved.ApprovalExpiresAt.UTC().Format(time.RFC3339))
						}
						if err := writeLine(cmd.OutOrStdout(), statusLine); err != nil {
							return err
						}
					} else {
						// Just display the details
						if err := writeLine(cmd.OutOrStdout(), "\nUse 'rack-gateway deploy-approval approve <id>' to approve this request."); err != nil {
							return err
						}
					}

					// Wait for sound to finish playing
					<-soundDone

					// If --loop is set, continue polling for more requests
					if !loop {
						return nil
					}

					// Print waiting message before next loop
					if err := printWaitingMessage(); err != nil {
						return err
					}

					// Continue polling after a brief pause
					time.Sleep(pollInterval)
				}

				time.Sleep(pollInterval)
			}
		}),
	}

	cmd.Flags().StringVar(&racksFlag, "racks", "", "Comma-separated list of rack names to monitor (e.g., dev,staging,prod)")
	cmd.Flags().StringVar(&pollIntervalStr, "poll-interval", "1s", "Polling interval")
	cmd.Flags().StringVar(&mfaCode, "mfa-code", "", "MFA code to satisfy step-up requirements")
	cmd.Flags().BoolVar(&autoApprove, "approve", false, "Automatically approve the first pending request found")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional notes for approval (only used with --approve)")
	cmd.Flags().BoolVar(&loop, "loop", false, "Continue polling for more requests after displaying or approving one")

	return cmd
}

func playNotificationSound(cfg *Config, rack string) error {
	// Determine notification sound preference (per-rack overrides global)
	soundPref := "default"
	if cfg != nil {
		// Check global setting
		if cfg.NotificationSound != "" {
			soundPref = cfg.NotificationSound
		}
		// Check per-rack override
		if rack != "" {
			if gwCfg, ok := cfg.Gateways[rack]; ok && gwCfg.NotificationSound != "" {
				soundPref = gwCfg.NotificationSound
			}
		}
	}

	// If disabled, return early
	if soundPref == "disabled" {
		return nil
	}

	var soundFile string
	var cleanupFile bool

	if soundPref == "default" || soundPref == "" {
		// Write embedded sound to temp file
		tmpFile, err := os.CreateTemp("", "notification-*.mp3")
		if err != nil {
			return err
		}
		soundFile = tmpFile.Name()
		cleanupFile = true
		defer func() {
			if cleanupFile {
				_ = os.Remove(soundFile)
			}
		}()

		if _, err := tmpFile.Write(notificationSound); err != nil {
			_ = tmpFile.Close()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			return err
		}
	} else {
		// Use custom sound file path
		soundFile = soundPref
		if _, err := os.Stat(soundFile); err != nil {
			return fmt.Errorf("notification sound file not found: %w", err)
		}
	}

	// Play sound based on OS
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("afplay", soundFile)
	case "linux":
		// Try common Linux audio players in order of preference
		for _, player := range []string{"paplay", "aplay", "ffplay", "mpg123"} {
			if _, err := exec.LookPath(player); err == nil {
				if player == "ffplay" {
					cmd = exec.Command(player, "-nodisp", "-autoexit", soundFile)
				} else {
					cmd = exec.Command(player, soundFile)
				}
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no audio player found (tried paplay, aplay, ffplay, mpg123)")
		}
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	return cmd.Run()
}

func createDeployApproval(cmd *cobra.Command, gatewayURL, bearer, rack, gitCommitHash, gitBranch, pipelineURL, ciProvider, message, targetToken string, mfaCode *string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{
		"message":         message,
		"git_commit_hash": gitCommitHash,
	}
	if trimmed := strings.TrimSpace(gitBranch); trimmed != "" {
		payload["git_branch"] = trimmed
	}
	if trimmed := strings.TrimSpace(pipelineURL); trimmed != "" {
		payload["pipeline_url"] = trimmed
	}
	if trimmed := strings.TrimSpace(ciProvider); trimmed != "" {
		payload["ci_provider"] = trimmed
	}
	if trimmed := strings.TrimSpace(targetToken); trimmed != "" {
		payload["target_api_token_id"] = trimmed
	}
	return postDeployApprovalRequest(cmd, gatewayURL, bearer, rack, "/deploy-approval-requests", payload, mfaCode)
}

func approveDeployRequest(cmd *cobra.Command, gatewayURL, bearer, rack, requestID, notes string, mfaCode *string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}
	return postDeployApprovalRequest(cmd, gatewayURL, bearer, rack, fmt.Sprintf("/admin/deploy-approval-requests/%s/approve", requestID), payload, mfaCode)
}

func postDeployApprovalRequest(cmd *cobra.Command, gatewayURL, bearer, rack, path string, payload map[string]interface{}, mfaCode *string) (*deployApprovalRequest, error) {
	fullPath := path
	attempts := 0
	for {
		attempts++
		resp, body, err := sendDeployApprovalRequest(gatewayURL, bearer, http.MethodPost, fullPath, payload)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
			if err := satisfyMFAStepUp(cmd, gatewayURL, bearer, rack, mfaCode); err != nil {
				return nil, err
			}
			if attempts < 3 {
				continue
			}
		}

		if resp.StatusCode >= 400 {
			if resp.StatusCode == http.StatusConflict {
				var existing deployApprovalRequest
				if len(body) > 0 && json.Unmarshal(body, &existing) == nil && existing.ID > 0 {
					return &existing, &deployApprovalRequestConflictError{request: &existing}
				}
			}
			return nil, fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var result deployApprovalRequest
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse deploy approval request response: %w", err)
		}
		return &result, nil
	}
}

func satisfyMFAStepUp(cmd *cobra.Command, gatewayURL, bearer, rack string, mfaCode *string) error {
	// If mfa-code flag provided, try TOTP verification
	if mfaCode != nil {
		code := strings.TrimSpace(*mfaCode)
		if code != "" {
			if err := submitMFAVerification(gatewayURL, bearer, code); err != nil {
				return fmt.Errorf("MFA verification failed: %w", err)
			}
			if err := writeLine(cmd.OutOrStdout(), "MFA verified."); err != nil {
				return err
			}
			*mfaCode = ""
			return nil
		}
	}
	// Use unified MFA module that respects preferences and --mfa-method flag
	return performMFAStepUp(cmd, gatewayURL, bearer, rack)
}

func waitForDeployApproval(cmd *cobra.Command, gatewayURL, bearer, rack string, id int64, interval, timeout time.Duration) (*deployApprovalRequest, error) {
	start := time.Now()
	var lastStatus string
	for {
		resp, body, err := sendDeployApprovalRequest(gatewayURL, bearer, http.MethodGet, fmt.Sprintf("/deploy-approval-requests/%d", id), nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
			// Use unified MFA module
			if err := performMFAStepUp(cmd, gatewayURL, bearer, rack); err != nil {
				return nil, err
			}
			resp, body, err = sendDeployApprovalRequest(gatewayURL, bearer, http.MethodGet, fmt.Sprintf("/deploy-approval-requests/%d", id), nil)
			if err != nil {
				return nil, err
			}
		}

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var result deployApprovalRequest
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse deploy approval request response: %w", err)
		}

		statusLower := strings.ToLower(result.Status)
		if statusLower == "approved" || statusLower == "rejected" || statusLower == "expired" {
			return &result, nil
		}

		if statusLower != lastStatus {
			if err := writef(cmd.OutOrStdout(), "Waiting for approval (status: %s)\n", result.Status); err != nil {
				return nil, err
			}
			lastStatus = statusLower
		}

		if timeout > 0 && time.Since(start) > timeout {
			return nil, fmt.Errorf("timed out waiting for approval")
		}

		time.Sleep(interval)
	}
}

func sendDeployApprovalRequest(gatewayURL, bearer, method, path string, payload interface{}) (*http.Response, []byte, error) {
	fullURL := strings.TrimRight(gatewayURL, "/") + "/.gateway/api" + path

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	return resp, respBody, nil
}

func isMFAStepUpRequired(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return strings.EqualFold(payload.Error, "mfa_step_up_required")
}
