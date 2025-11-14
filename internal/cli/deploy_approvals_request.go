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
	var opts deployApprovalRequestOptions

	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request manual approval for CI/CD deploy",
		Args:  cobra.NoArgs,
		RunE: SilenceOnError(func(cmd *cobra.Command, _ []string) error {
			cfg, err := parseDeployApprovalRequestOptions(cmd, opts)
			if err != nil {
				return err
			}
			return executeDeployApprovalRequest(cmd, cfg)
		}),
	}

	cmd.Flags().
		StringVarP(&opts.appFlag, "app", "a", "", "App name (auto-detected from .convox/app or current directory)")
	cmd.Flags().StringVar(&opts.rackFlag, "rack", "", "Rack name")
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "Block until approval is decided")
	cmd.Flags().StringVar(&opts.pollInterval, "poll-interval", "5s", "Polling interval when --wait is set")
	cmd.Flags().StringVar(
		&opts.timeout,
		"timeout",
		"20m",
		"Maximum time to wait before giving up (set to 0 to wait indefinitely)",
	)
	cmd.Flags().StringVar(&opts.gitCommitHash, "git-commit", "", "Git commit SHA (required)")
	cmd.Flags().StringVar(&opts.gitBranch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(
		&opts.ciMetadata,
		"ci-metadata",
		"",
		"CI metadata as JSON (e.g., '{\"workflow_id\":\"abc123\",\"pipeline_number\":\"456\"}')",
	)
	cmd.Flags().StringVar(&opts.message, "message", "", "Deploy approval message (required)")

	_ = cmd.MarkFlagRequired("git-commit")
	_ = cmd.MarkFlagRequired("message")

	return cmd
}

type deployApprovalRequestOptions struct {
	rackFlag      string
	appFlag       string
	wait          bool
	pollInterval  string
	timeout       string
	gitCommitHash string
	gitBranch     string
	ciMetadata    string
	message       string
}

type deployApprovalRequestConfig struct {
	rack          string
	app           string
	wait          bool
	pollInterval  time.Duration
	timeout       time.Duration
	gitCommitHash string
	gitBranch     string
	ciMetadata    map[string]interface{}
	message       string
}

func parseDeployApprovalRequestOptions(
	_ *cobra.Command,
	opts deployApprovalRequestOptions,
) (deployApprovalRequestConfig, error) {
	commit := strings.TrimSpace(opts.gitCommitHash)
	if commit == "" {
		return deployApprovalRequestConfig{}, fmt.Errorf("--git-commit is required")
	}

	message := strings.TrimSpace(opts.message)
	if message == "" {
		return deployApprovalRequestConfig{}, fmt.Errorf("--message is required")
	}

	app, err := ResolveApp(opts.appFlag)
	if err != nil {
		return deployApprovalRequestConfig{}, err
	}

	rack, err := resolveRackFlag(opts.rackFlag)
	if err != nil {
		return deployApprovalRequestConfig{}, err
	}

	pollInterval, err := parseDurationFlag(opts.pollInterval, "poll-interval", false, 5*time.Second)
	if err != nil {
		return deployApprovalRequestConfig{}, err
	}

	timeout, err := parseDurationFlag(opts.timeout, "timeout", true, 0)
	if err != nil {
		return deployApprovalRequestConfig{}, err
	}

	metadata, err := parseCIMetadata(opts.ciMetadata)
	if err != nil {
		return deployApprovalRequestConfig{}, err
	}

	return deployApprovalRequestConfig{
		rack:          rack,
		app:           app,
		wait:          opts.wait,
		pollInterval:  pollInterval,
		timeout:       timeout,
		gitCommitHash: commit,
		gitBranch:     strings.TrimSpace(opts.gitBranch),
		ciMetadata:    metadata,
		message:       message,
	}, nil
}

func resolveRackFlag(flagValue string) (string, error) {
	rack, err := SelectedRack()
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(flagValue)
	if trimmed != "" {
		return trimmed, nil
	}
	return rack, nil
}

func parseCIMetadata(raw string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &metadata); err != nil {
		return nil, fmt.Errorf("invalid --ci-metadata JSON: %w", err)
	}
	return metadata, nil
}

func executeDeployApprovalRequest(cmd *cobra.Command, cfg deployApprovalRequestConfig) error {
	created, err := createDeployApproval(
		cmd,
		cfg.rack,
		cfg.app,
		cfg.gitCommitHash,
		cfg.gitBranch,
		cfg.ciMetadata,
		cfg.message,
		"",
	)
	if err != nil {
		return handleDeployApprovalCreationError(cmd, err)
	}
	if created == nil {
		return fmt.Errorf("failed to create deploy approval request")
	}

	err = writef(
		cmd.OutOrStdout(),
		"Deploy approval request %s created (status: %s)\n",
		created.PublicID,
		created.Status,
	)
	if err != nil {
		return err
	}

	if !cfg.wait {
		return nil
	}

	final, err := waitForDeployApproval(cmd, cfg.rack, created.PublicID, cfg.pollInterval, cfg.timeout)
	if err != nil {
		return err
	}

	return reportFinalApprovalStatus(cmd, final)
}

func handleDeployApprovalCreationError(cmd *cobra.Command, err error) error {
	var conflict *deployApprovalRequestConflictError
	if errors.As(err, &conflict) {
		return writeLine(cmd.OutOrStdout(), "Deploy approval request already exists for this commit")
	}
	return err
}

func reportFinalApprovalStatus(cmd *cobra.Command, final *deployApprovalRequest) error {
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

func waitForDeployApproval(
	cmd *cobra.Command,
	rack, publicID string,
	interval, timeout time.Duration,
) (*deployApprovalRequest, error) {
	start := time.Now()
	var lastStatus string
	for {
		var result deployApprovalRequest
		endpoint := fmt.Sprintf("/deploy-approval-requests/%s", publicID)
		if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
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
