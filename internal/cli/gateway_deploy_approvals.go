package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
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
	PublicID           string                 `json:"public_id"`
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
		appFlag         string
		waitFlag        bool
		pollIntervalStr string
		timeoutStr      string
		gitCommitHash   string
		gitBranch       string
		ciMetadata      string
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

			// Resolve app using auto-detection
			app, err := ResolveApp(appFlag)
			if err != nil {
				return err
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

			// Parse CI metadata JSON if provided
			var ciMetadataMap map[string]interface{}
			if trimmed := strings.TrimSpace(ciMetadata); trimmed != "" {
				if err := json.Unmarshal([]byte(trimmed), &ciMetadataMap); err != nil {
					return fmt.Errorf("invalid --ci-metadata JSON: %w", err)
				}
			}

			created, err := createDeployApproval(cmd, rack, app, gitCommitHash, gitBranch, ciMetadataMap, message, "")
			if err != nil {
				var conflict *deployApprovalRequestConflictError
				if errors.As(err, &conflict) {
					if err := writeLine(cmd.OutOrStdout(), "Deploy approval request already exists for this commit"); err != nil {
						return err
					}
					// For conflict errors, we don't have the existing request details
					// User can check status with: rack-gateway deploy-approval list
					return nil
				}
				return err
			}

			if err := writef(cmd.OutOrStdout(), "Deploy approval request %s created (status: %s)\n", created.PublicID, created.Status); err != nil {
				return err
			}

			if created == nil {
				return fmt.Errorf("failed to create deploy approval request")
			}

			if waitFlag {
				final, err := waitForDeployApproval(cmd, rack, created.PublicID, pollInterval, timeout)
				if err != nil {
					return err
				}
				switch strings.ToLower(final.Status) {
				case "approved", "expired":
					if err := writef(cmd.OutOrStdout(), "Deploy approval request %s approved.\n", final.PublicID); err != nil {
						return err
					}
					return nil
				case "rejected":
					note := strings.TrimSpace(final.ApprovalNotes)
					if note != "" {
						return fmt.Errorf("deploy approval request %s rejected: %s", final.PublicID, note)
					}
					return fmt.Errorf("deploy approval request %s rejected", final.PublicID)
				default:
					return fmt.Errorf("deploy approval request %s finished with status: %s", final.PublicID, final.Status)
				}
			}

			return nil
		}),
	}

	cmd.Flags().StringVarP(&appFlag, "app", "a", "", "App name (auto-detected from .convox/app or current directory)")
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
	cmd.Flags().StringVar(&ciMetadata, "ci-metadata", "", "CI metadata as JSON (e.g., '{\"workflow_id\":\"abc123\",\"pipeline_number\":\"456\"}')")
	cmd.Flags().StringVar(&message, "message", "", "Deploy approval message (required)")

	_ = cmd.MarkFlagRequired("git-commit")
	_ = cmd.MarkFlagRequired("message")

	return cmd
}

func newDeployApprovalApproveCommand() *cobra.Command {
	var (
		rackFlag string
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

			approved, err := approveDeployRequest(cmd, rack, requestID, strings.TrimSpace(notes))
			if err != nil {
				return err
			}

			statusLine := fmt.Sprintf("Deploy approval request %s approved", approved.PublicID)
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
	cmd.Flags().StringVar(&notes, "notes", "", "Optional notes for approval")

	return cmd
}

func newDeployApprovalWaitCommand() *cobra.Command {
	var (
		racksFlag       string
		pollIntervalStr string
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

			// Prepare rack list for round-robin polling
			type rackInfo struct {
				name string
			}
			rackInfos := make([]rackInfo, 0, len(racks))
			for _, rack := range racks {
				rackInfos = append(rackInfos, rackInfo{
					name: rack,
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
				var result struct {
					Requests []deployApprovalRequest `json:"deploy_approval_requests"`
				}
				if err := gatewayRequest(cmd, info.name, http.MethodGet, "/deploy-approval-requests?status=pending", nil, &result); err != nil {
					return err
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
					if err := writef(cmd.OutOrStdout(), "  ID: %s\n", req.PublicID); err != nil {
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
						// Approve the request (MFA will be prompted if needed)
						approved, err := approveDeployRequest(cmd, info.name, req.PublicID, strings.TrimSpace(notes))
						if err != nil {
							return err
						}

						statusLine := fmt.Sprintf("\n✅ Deploy approval request %s approved", approved.PublicID)
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

func createDeployApproval(cmd *cobra.Command, rack, app, gitCommitHash, gitBranch string, ciMetadata map[string]interface{}, message, targetToken string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{
		"message":         message,
		"git_commit_hash": gitCommitHash,
	}
	if trimmed := strings.TrimSpace(app); trimmed != "" {
		payload["app"] = trimmed
	}
	if trimmed := strings.TrimSpace(gitBranch); trimmed != "" {
		payload["git_branch"] = trimmed
	}
	if len(ciMetadata) > 0 {
		payload["ci_metadata"] = ciMetadata
	}
	if trimmed := strings.TrimSpace(targetToken); trimmed != "" {
		payload["target_api_token_id"] = trimmed
	}

	var result deployApprovalRequest
	if err := gatewayRequest(cmd, rack, http.MethodPost, "/deploy-approval-requests", payload, &result); err != nil {
		// Handle conflict error specially
		if strings.Contains(err.Error(), "409") {
			return &result, &deployApprovalRequestConflictError{request: &result}
		}
		return nil, err
	}
	return &result, nil
}

func approveDeployRequest(cmd *cobra.Command, rack, requestID, notes string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}

	var result deployApprovalRequest
	if err := gatewayRequest(cmd, rack, http.MethodPost, fmt.Sprintf("/deploy-approval-requests/%s/approve", requestID), payload, &result); err != nil {
		// Handle conflict error specially
		if strings.Contains(err.Error(), "409") {
			return &result, &deployApprovalRequestConflictError{request: &result}
		}
		return nil, err
	}
	return &result, nil
}

func waitForDeployApproval(cmd *cobra.Command, rack, publicID string, interval, timeout time.Duration) (*deployApprovalRequest, error) {
	start := time.Now()
	var lastStatus string
	for {
		var result deployApprovalRequest
		if err := gatewayRequest(cmd, rack, http.MethodGet, fmt.Sprintf("/deploy-approval-requests/%s", publicID), nil, &result); err != nil {
			return nil, err
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
