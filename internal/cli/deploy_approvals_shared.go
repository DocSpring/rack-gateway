package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

//go:embed assets/notification.mp3
var notificationSound []byte

// ANSI color codes
const (
	colorReset     = "\033[0m"
	colorBold      = "\033[1m"
	colorDim       = "\033[2m"
	colorCyan      = "\033[96m" // bright cyan
	colorGreen     = "\033[92m" // light green
	colorRed       = "\033[91m" // light red/pink
	colorYellow    = "\033[93m"
	colorGray      = "\033[38;5;245m" // medium gray (256-color)
	colorBoldGreen = "\033[1;92m"
	colorBoldRed   = "\033[1;91m"
)

// colorsEnabled returns true if stdout is a terminal and colors should be used
func colorsEnabled() bool {
	fd := os.Stdout.Fd()
	// Validate fd fits in int (always true on supported platforms, but validates for gosec)
	const maxInt = int(^uint(0) >> 1)
	if fd > uintptr(maxInt) {
		return false
	}
	return term.IsTerminal(int(fd))
}

// dim returns dimmed/gray text
func dim(s string) string {
	if !colorsEnabled() {
		return s
	}
	return colorGray + s + colorReset
}

// statusColor returns the status with appropriate color
func statusColor(status string) string {
	if !colorsEnabled() {
		return status
	}
	switch strings.ToLower(status) {
	case "pending":
		return colorCyan + status + colorReset
	case "approved":
		return colorBoldGreen + status + colorReset
	case "rejected":
		return colorBoldRed + status + colorReset
	case "expired":
		return colorYellow + status + colorReset
	default:
		return status
	}
}

type deployApprovalRequest struct {
	PublicID           string                 `json:"public_id"`
	Message            string                 `json:"message"`
	Status             string                 `json:"status"`
	App                string                 `json:"app,omitempty"`
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

func (_ *deployApprovalRequestConflictError) Error() string {
	return "deploy approval request already exists"
}

func postDeployApprovalRequest(
	cmd *cobra.Command,
	rack, endpoint string,
	payload map[string]interface{},
) (*deployApprovalRequest, error) {
	var result deployApprovalRequest
	if err := gatewayRequest(cmd, rack, http.MethodPost, endpoint, payload, &result); err != nil {
		if isGatewayStatus(err, http.StatusConflict) {
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

// resolveRacks parses the selected rack. Deploy approval commands accept comma-separated rack names
// through the global --rack flag. Special value "all" expands to all configured racks.
func resolveRacks() ([]string, error) {
	rack, err := SelectedRack()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(rack)
	if trimmed == "all" {
		return expandAllRacks()
	}

	racks, err := parseRackList(trimmed)
	if err != nil {
		return nil, err
	}
	if err := validateConfiguredRacks(racks); err != nil {
		return nil, err
	}
	return racks, nil
}

func parseRackList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	racks := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			racks = append(racks, value)
		}
	}
	if len(racks) == 0 {
		return nil, fmt.Errorf("no valid rack names provided")
	}
	return racks, nil
}

// expandAllRacks returns all configured racks except those in all_racks_exclude.
func expandAllRacks() ([]string, error) {
	cfg, _, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if len(cfg.Gateways) == 0 {
		return nil, fmt.Errorf("no racks configured")
	}

	// Get all configured rack names
	allRacks := make([]string, 0, len(cfg.Gateways))
	for rackName := range cfg.Gateways {
		allRacks = append(allRacks, rackName)
	}

	// Filter out excluded racks
	return filterExcludedRacks(allRacks, cfg.AllRacksExclude), nil
}

// filterExcludedRacks filters out racks that are in the exclude list.
func filterExcludedRacks(racks []string, exclude []string) []string {
	if len(exclude) == 0 {
		return racks
	}

	excludeMap := make(map[string]bool, len(exclude))
	for _, excluded := range exclude {
		excludeMap[excluded] = true
	}

	filtered := make([]string, 0, len(racks))
	for _, rack := range racks {
		if !excludeMap[rack] {
			filtered = append(filtered, rack)
		}
	}
	return filtered
}

type missingRackError struct {
	name string
}

func (e *missingRackError) Error() string {
	return fmt.Sprintf("No rack found with name %q", e.name)
}

func validateConfiguredRacks(racks []string) error {
	if strings.TrimSpace(os.Getenv("RACK_GATEWAY_URL")) != "" {
		return nil
	}

	cfg, exists, err := LoadConfig()
	if err != nil {
		return err
	}
	for _, rack := range racks {
		if !exists || cfg.Gateways == nil {
			return &missingRackError{name: rack}
		}
		if _, ok := cfg.Gateways[rack]; !ok {
			return &missingRackError{name: rack}
		}
	}
	return nil
}

// getCurrentGitCommit returns the current git commit hash (short form), or an error if not in a git repo.
func getCurrentGitCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current git commit: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// resolveBranchOrCommit resolves the branch and commit values from the provided options.
// If neither is specified, it falls back to the current git commit.
func resolveBranchOrCommit(branchOpt, commitOpt string) (branch, commit string, err error) {
	branch = strings.TrimSpace(branchOpt)
	commit = strings.TrimSpace(commitOpt)

	if branch == "" && commit == "" {
		currentCommit, gitErr := getCurrentGitCommit()
		if gitErr != nil {
			return "", "", fmt.Errorf("no ID, --branch, or --commit provided, and %w", gitErr)
		}
		commit = currentCommit
	}
	return branch, commit, nil
}

type deployApprovalRequestList struct {
	DeployApprovalRequests []deployApprovalRequest `json:"deploy_approval_requests"`
}

// searchForRequestInRack searches for a deploy approval request in a specific rack.
func searchForRequestInRack(
	cmd *cobra.Command, rack, app, branch, commit, status string,
) (*deployApprovalRequest, bool, error) {
	params := url.Values{}
	params.Set("status", status)
	params.Set("limit", "1")
	params.Set("app", app)
	if branch != "" {
		params.Set("git_branch", branch)
	}
	if commit != "" {
		params.Set("git_commit", commit)
	}
	endpoint := "/deploy-approval-requests?" + params.Encode()

	var result deployApprovalRequestList
	if err := gatewayRequest(cmd, rack, http.MethodGet, endpoint, nil, &result); err != nil {
		return nil, false, err
	}
	if len(result.DeployApprovalRequests) > 0 {
		return &result.DeployApprovalRequests[0], true, nil
	}
	return nil, false, nil
}

func rackScopedError(rack string, err error, rackCount int) error {
	if errors.Is(err, ErrTokenExpired) {
		return err
	}
	if rackCount <= 1 {
		return err
	}
	return fmt.Errorf("rack %s: %w", rack, err)
}

func isGatewayStatus(err error, statusCode int) bool {
	var gatewayErr *gatewayHTTPError
	return errors.As(err, &gatewayErr) && gatewayErr.statusCode == statusCode
}
