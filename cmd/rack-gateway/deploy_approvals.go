package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type deployApprovalRequest struct {
	ID                 int64      `json:"id"`
	Message            string     `json:"message"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	TargetAPITokenID   string     `json:"target_api_token_id"`
	TargetAPITokenName string     `json:"target_api_token_name,omitempty"`
	ApprovedAt         *time.Time `json:"approved_at,omitempty"`
	ApprovalExpiresAt  *time.Time `json:"approval_expires_at,omitempty"`
	RejectedAt         *time.Time `json:"rejected_at,omitempty"`
	ApprovalNotes      string     `json:"approval_notes,omitempty"`
}

type deployApprovalRequestConflictError struct {
	request *deployApprovalRequest
}

func (e *deployApprovalRequestConflictError) Error() string {
	return "deploy approval request already exists"
}

func deployApprovalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-approval",
		Short: "Manage deploy approvals",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newDeployApprovalRequestCommand(), newDeployApprovalApproveCommand())

	return cmd
}

func newDeployApprovalRequestCommand() *cobra.Command {
	var (
		rackFlag        string
		waitFlag        bool
		pollIntervalStr string
		timeoutStr      string
	)

	cmd := &cobra.Command{
		Use:   "request <app> <release_id> <message>",
		Short: "Request manual approval for CI/CD deploy",
		Args:  cobra.ExactArgs(3),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			app := strings.TrimSpace(args[0])
			if app == "" {
				return fmt.Errorf("app is required")
			}

			releaseID := strings.TrimSpace(args[1])
			if releaseID == "" {
				return fmt.Errorf("release_id is required")
			}

			message := strings.TrimSpace(args[2])
			if message == "" {
				return fmt.Errorf("message is required")
			}

			rack, err := selectedRack()
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

			created, err := createDeployApproval(cmd, gatewayURL, bearer, app, releaseID, message, "", nil)
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
				final, err := waitForDeployApproval(cmd, gatewayURL, bearer, created.ID, pollInterval, timeout)
				if err != nil {
					return err
				}
				switch strings.ToLower(final.Status) {
				case "approved", "consumed":
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
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			requestID := strings.TrimSpace(args[0])
			if requestID == "" {
				return fmt.Errorf("request_id is required")
			}

			rack, err := selectedRack()
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

			approved, err := approveDeployRequest(cmd, gatewayURL, bearer, requestID, strings.TrimSpace(notes), &mfaCode)
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

func createDeployApproval(cmd *cobra.Command, gatewayURL, bearer, app, releaseID, message, targetToken string, mfaCode *string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{
		"message":    message,
		"app":        app,
		"release_id": releaseID,
	}
	if trimmed := strings.TrimSpace(targetToken); trimmed != "" {
		payload["target_api_token_id"] = trimmed
	}
	return postDeployApprovalRequest(cmd, gatewayURL, bearer, "/deploy-approval-requests", payload, mfaCode)
}

func approveDeployRequest(cmd *cobra.Command, gatewayURL, bearer, requestID, notes string, mfaCode *string) (*deployApprovalRequest, error) {
	payload := map[string]interface{}{}
	if notes != "" {
		payload["notes"] = notes
	}
	return postDeployApprovalRequest(cmd, gatewayURL, bearer, fmt.Sprintf("/admin/deploy-approval-requests/%s/approve", requestID), payload, mfaCode)
}

func postDeployApprovalRequest(cmd *cobra.Command, gatewayURL, bearer, path string, payload map[string]interface{}, mfaCode *string) (*deployApprovalRequest, error) {
	fullPath := path
	attempts := 0
	for {
		attempts++
		resp, body, err := sendDeployApprovalRequest(gatewayURL, bearer, http.MethodPost, fullPath, payload)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
			if err := satisfyMFAStepUp(cmd, gatewayURL, bearer, mfaCode); err != nil {
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

func satisfyMFAStepUp(cmd *cobra.Command, gatewayURL, bearer string, mfaCode *string) error {
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
	return promptAndVerifyMFA(cmd, gatewayURL, bearer)
}

func waitForDeployApproval(cmd *cobra.Command, gatewayURL, bearer string, id int64, interval, timeout time.Duration) (*deployApprovalRequest, error) {
	start := time.Now()
	var lastStatus string
	for {
		resp, body, err := sendDeployApprovalRequest(gatewayURL, bearer, http.MethodGet, fmt.Sprintf("/deploy-approval-requests/%d", id), nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
			if err := promptAndVerifyMFA(cmd, gatewayURL, bearer); err != nil {
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
		if statusLower == "approved" || statusLower == "rejected" || statusLower == "consumed" {
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

func promptAndVerifyMFA(cmd *cobra.Command, gatewayURL, bearer string) error {
	if err := writeLine(cmd.OutOrStdout(), "Multi-factor authentication required."); err != nil {
		return err
	}
	for attempts := 0; attempts < 5; attempts++ {
		code, err := promptMFACode()
		if err != nil {
			return err
		}
		if code == "" {
			if err := writeLine(cmd.OutOrStdout(), "MFA code cannot be empty."); err != nil {
				return err
			}
			continue
		}
		if err := submitMFAVerification(gatewayURL, bearer, code); err != nil {
			if err := writef(cmd.OutOrStdout(), "MFA verification failed: %v\n", err); err != nil {
				return err
			}
			continue
		}
		if err := writeLine(cmd.OutOrStdout(), "MFA verified."); err != nil {
			return err
		}
		return nil
	}
	return errors.New("failed to verify MFA after multiple attempts")
}
