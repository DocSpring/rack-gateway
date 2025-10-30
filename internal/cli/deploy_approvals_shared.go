package cli

import (
	_ "embed"
	"fmt"
	"net/http"
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

func postDeployApprovalRequest(
	cmd *cobra.Command,
	rack, endpoint string,
	payload map[string]interface{},
) (*deployApprovalRequest, error) {
	var result deployApprovalRequest
	if err := gatewayRequest(cmd, rack, http.MethodPost, endpoint, payload, &result); err != nil {
		if strings.Contains(err.Error(), "409") {
			return &result, &deployApprovalRequestConflictError{request: &result}
		}
		return nil, err
	}
	return &result, nil
}

func parseDurationFlag(raw, flag string, allowZero bool, defaultValue time.Duration) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultValue, nil
	}

	dur, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid --%s: %w", flag, err)
	}
	if !allowZero && dur <= 0 {
		return 0, fmt.Errorf("--%s must be positive", flag)
	}

	return dur, nil
}
