package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

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
			gitCommitHash = strings.TrimSpace(gitCommitHash)
			if gitCommitHash == "" {
				return fmt.Errorf("--git-commit is required")
			}

			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("--message is required")
			}

			app, err := ResolveApp(appFlag)
			if err != nil {
				return err
			}

			rack, err := SelectedRack()
			if err != nil {
				return err
			}
			if trimmedRack := strings.TrimSpace(rackFlag); trimmedRack != "" {
				rack = trimmedRack
			}

			pollInterval, err := parseDurationFlag(pollIntervalStr, "poll-interval", false, 5*time.Second)
			if err != nil {
				return err
			}

			timeout, err := parseDurationFlag(timeoutStr, "timeout", true, 0)
			if err != nil {
				return err
			}

			var ciMetadataMap map[string]interface{}
			if trimmed := strings.TrimSpace(ciMetadata); trimmed != "" {
				if err := json.Unmarshal([]byte(trimmed), &ciMetadataMap); err != nil {
					return fmt.Errorf("invalid --ci-metadata JSON: %w", err)
				}
			}

			created, err := createDeployApproval(
				cmd,
				rack,
				app,
				gitCommitHash,
				gitBranch,
				ciMetadataMap,
				message,
				"",
			)
			if err != nil {
				var conflict *deployApprovalRequestConflictError
				if errors.As(err, &conflict) {
					if err := writeLine(cmd.OutOrStdout(), "Deploy approval request already exists for this commit"); err != nil {
						return err
					}
					return nil
				}
				return err
			}

			if created == nil {
				return fmt.Errorf("failed to create deploy approval request")
			}

			if err := writef(cmd.OutOrStdout(), "Deploy approval request %s created (status: %s)\n", created.PublicID, created.Status); err != nil {
				return err
			}

			if !waitFlag {
				return nil
			}

			final, err := waitForDeployApproval(cmd, rack, created.PublicID, pollInterval, timeout)
			if err != nil {
				return err
			}

			switch strings.ToLower(final.Status) {
			case "approved", "expired":
				return writef(cmd.OutOrStdout(), "Deploy approval request %s approved.\n", final.PublicID)
			case "rejected":
				note := strings.TrimSpace(final.ApprovalNotes)
				if note != "" {
					return fmt.Errorf("deploy approval request %s rejected: %s", final.PublicID, note)
				}
				return fmt.Errorf("deploy approval request %s rejected", final.PublicID)
			default:
				return fmt.Errorf("deploy approval request %s finished with status: %s", final.PublicID, final.Status)
			}
		}),
	}

	cmd.Flags().StringVarP(&appFlag, "app", "a", "", "App name (auto-detected from .convox/app or current directory)")
	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name")
	cmd.Flags().BoolVar(&waitFlag, "wait", false, "Block until approval is decided")
	cmd.Flags().StringVar(&pollIntervalStr, "poll-interval", "5s", "Polling interval when --wait is set")
	cmd.Flags().StringVar(&timeoutStr, "timeout", "20m", "Maximum time to wait before giving up (set to 0 to wait indefinitely)")
	cmd.Flags().StringVar(&gitCommitHash, "git-commit", "", "Git commit SHA (required)")
	cmd.Flags().StringVar(&gitBranch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&ciMetadata, "ci-metadata", "", "CI metadata as JSON (e.g., '{\"workflow_id\":\"abc123\",\"pipeline_number\":\"456\"}')")
	cmd.Flags().StringVar(&message, "message", "", "Deploy approval message (required)")

	_ = cmd.MarkFlagRequired("git-commit")
	_ = cmd.MarkFlagRequired("message")

	return cmd
}

func createDeployApproval(
	cmd *cobra.Command,
	rack,
	app,
	gitCommitHash,
	gitBranch string,
	ciMetadata map[string]interface{},
	message,
	targetToken string,
) (*deployApprovalRequest, error) {
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

	return postDeployApprovalRequest(cmd, rack, "/deploy-approval-requests", payload)
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
