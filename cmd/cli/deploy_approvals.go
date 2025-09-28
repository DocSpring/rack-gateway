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

type deployRequest struct {
	ID                 int64      `json:"id"`
	Rack               string     `json:"rack"`
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

type deployRequestConflictError struct {
	request *deployRequest
}

func (e *deployRequestConflictError) Error() string {
	return "deploy request already exists"
}

func newDeployApprovalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-approval",
		Short: "Manage deploy approvals",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newDeployApprovalRequestCommand(), newDeployApprovalPreApproveCommand())

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
		Use:   "request <message>",
		Short: "Request manual approval for CI/CD deploy",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			message := strings.TrimSpace(args[0])
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

			created, err := createDeployApproval(cmd, rack, gatewayURL, bearer, message, "", nil)
			if err != nil {
				var conflict *deployRequestConflictError
				if errors.As(err, &conflict) && conflict.request != nil {
					created = conflict.request
					if err := writef(cmd.OutOrStdout(), "Deploy request %d already exists (status: %s)\n", created.ID, created.Status); err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				if err := writef(cmd.OutOrStdout(), "Deploy request %d created (status: %s)\n", created.ID, created.Status); err != nil {
					return err
				}
			}

			if created == nil {
				return fmt.Errorf("failed to create deploy request")
			}

			if waitFlag {
				final, err := waitForDeployApproval(cmd, rack, gatewayURL, bearer, created.ID, pollInterval, timeout)
				if err != nil {
					return err
				}
				switch strings.ToLower(final.Status) {
				case "approved", "consumed":
					if err := writef(cmd.OutOrStdout(), "Deploy request %d approved.\n", final.ID); err != nil {
						return err
					}
					return nil
				case "rejected":
					note := strings.TrimSpace(final.ApprovalNotes)
					if note != "" {
						return fmt.Errorf("deploy request %d rejected: %s", final.ID, note)
					}
					return fmt.Errorf("deploy request %d rejected", final.ID)
				default:
					return fmt.Errorf("deploy request %d finished with status: %s", final.ID, final.Status)
				}
			}

			return nil
		}),
	}

	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name override")
	cmd.Flags().BoolVar(&waitFlag, "wait", false, "Block until approval is decided")
	cmd.Flags().StringVar(&pollIntervalStr, "poll-interval", "5s", "Polling interval when --wait is set")
	cmd.Flags().StringVar(&timeoutStr, "timeout", "", "Maximum time to wait; empty for indefinite")

	return cmd
}

func newDeployApprovalPreApproveCommand() *cobra.Command {
	var (
		targetTokenID string
		rackFlag      string
		mfaCode       string
	)

	cmd := &cobra.Command{
		Use:   "pre-approve <message>",
		Short: "Create and immediately approve a deploy request for a CI/CD token",
		Args:  cobra.ExactArgs(1),
		RunE: silenceOnError(func(cmd *cobra.Command, args []string) error {
			message := strings.TrimSpace(args[0])
			if message == "" {
				return fmt.Errorf("message is required")
			}

			trimmedTarget := strings.TrimSpace(targetTokenID)
			if trimmedTarget == "" {
				return fmt.Errorf("--target-api-token-id is required")
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

			created, err := preapproveDeploy(cmd, rack, gatewayURL, bearer, message, trimmedTarget, &mfaCode)
			if err != nil {
				return err
			}

			statusLine := fmt.Sprintf("Deploy request %d pre-approved", created.ID)
			if created.ApprovalExpiresAt != nil {
				statusLine = fmt.Sprintf("%s (expires at %s)", statusLine, created.ApprovalExpiresAt.UTC().Format(time.RFC3339))
			}
			if err := writeLine(cmd.OutOrStdout(), statusLine); err != nil {
				return err
			}
			return nil
		}),
	}

	cmd.Flags().StringVar(&targetTokenID, "target-api-token-id", "", "Target API token ID (public UUID)")
	cmd.Flags().StringVar(&rackFlag, "rack", "", "Rack name override")
	cmd.Flags().StringVar(&mfaCode, "mfa-code", "", "MFA code to satisfy step-up requirements")
	if err := cmd.MarkFlagRequired("target-api-token-id"); err != nil {
		panic(err)
	}

	return cmd
}

func createDeployApproval(cmd *cobra.Command, rack, gatewayURL, bearer, message, targetToken string, mfaCode *string) (*deployRequest, error) {
	payload := map[string]interface{}{
		"message": message,
		"rack":    rack,
	}
	if trimmed := strings.TrimSpace(targetToken); trimmed != "" {
		payload["target_api_token_id"] = trimmed
	}
	return postDeployRequest(cmd, gatewayURL, bearer, "/deploy-requests", payload, mfaCode)
}

func preapproveDeploy(cmd *cobra.Command, rack, gatewayURL, bearer, message, targetToken string, mfaCode *string) (*deployRequest, error) {
	payload := map[string]interface{}{
		"message":             message,
		"rack":                rack,
		"target_api_token_id": strings.TrimSpace(targetToken),
	}
	return postDeployRequest(cmd, gatewayURL, bearer, "/admin/deploy-requests/preapprove", payload, mfaCode)
}

func postDeployRequest(cmd *cobra.Command, gatewayURL, bearer, path string, payload map[string]interface{}, mfaCode *string) (*deployRequest, error) {
	fullPath := path
	attempts := 0
	for {
		attempts++
		resp, body, err := sendDeployRequest(gatewayURL, bearer, http.MethodPost, fullPath, payload)
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
				var existing deployRequest
				if len(body) > 0 && json.Unmarshal(body, &existing) == nil && existing.ID > 0 {
					return &existing, &deployRequestConflictError{request: &existing}
				}
			}
			return nil, fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var result deployRequest
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse deploy request response: %w", err)
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

func waitForDeployApproval(cmd *cobra.Command, rack, gatewayURL, bearer string, id int64, interval, timeout time.Duration) (*deployRequest, error) {
	start := time.Now()
	var lastStatus string
	for {
		resp, body, err := sendDeployRequest(gatewayURL, bearer, http.MethodGet, fmt.Sprintf("/deploy-requests/%d", id), nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && isMFAStepUpRequired(body) {
			if err := promptAndVerifyMFA(cmd, gatewayURL, bearer); err != nil {
				return nil, err
			}
			resp, body, err = sendDeployRequest(gatewayURL, bearer, http.MethodGet, fmt.Sprintf("/deploy-requests/%d", id), nil)
			if err != nil {
				return nil, err
			}
		}

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var result deployRequest
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse deploy request response: %w", err)
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

func sendDeployRequest(gatewayURL, bearer, method, path string, payload interface{}) (*http.Response, []byte, error) {
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
