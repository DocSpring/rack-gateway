package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

const (
	// deployRequestPageSize is the most requests the gateway lists at once.
	deployRequestPageSize = 200
	// maxDeployRequestPages stops a search that would never end (a gateway that ignores the offset).
	maxDeployRequestPages = 50
)

// fetchDeployRequestsByStatus returns, newest first, the newest request of each app on rack with the given status
// that matches the app, branch and commit filters. It reads every page, so no app is left out however many match.
// An approval past its expiry no longer allows a deploy, so it isn't returned as approved, even before the
// gateway's sweep marks it expired.
func fetchDeployRequestsByStatus(
	cmd *cobra.Command, rack, app, branch, commit, status string,
) ([]deployApprovalRequest, error) {
	params := url.Values{}
	params.Set("status", status)
	params.Set("newest_per_app", "true")
	params.Set("limit", strconv.Itoa(deployRequestPageSize))
	if app != "" {
		params.Set("app", app)
	}
	if branch != "" {
		params.Set("git_branch", branch)
	}
	if commit != "" {
		params.Set("git_commit", commit)
	}

	var requests []deployApprovalRequest
	for page := range maxDeployRequestPages {
		params.Set("offset", strconv.Itoa(page*deployRequestPageSize))
		found, err := fetchDeployRequestPage(cmd, rack, params)
		if err != nil {
			return nil, err
		}
		requests = append(requests, found...)
		if len(found) < deployRequestPageSize {
			if status == "approved" {
				return withoutExpiredApprovals(requests, time.Now()), nil
			}
			return requests, nil
		}
	}
	return nil, fmt.Errorf("more than %d %s deploy approval requests match; narrow the search with --app or --commit",
		maxDeployRequestPages*deployRequestPageSize, status)
}

func fetchDeployRequestPage(cmd *cobra.Command, rack string, params url.Values) ([]deployApprovalRequest, error) {
	var response struct {
		Requests []deployApprovalRequest `json:"deploy_approval_requests"`
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

// withoutExpiredApprovals drops the approvals whose window ended before now.
func withoutExpiredApprovals(requests []deployApprovalRequest, now time.Time) []deployApprovalRequest {
	var current []deployApprovalRequest
	for _, req := range requests {
		if req.ApprovalExpiresAt == nil || req.ApprovalExpiresAt.After(now) {
			current = append(current, req)
		}
	}
	return current
}
