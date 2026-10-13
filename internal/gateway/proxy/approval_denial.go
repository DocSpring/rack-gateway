package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// approvalDenial is the 403 for an API token whose deploy_with_approval didn't cover the request. It says why, from
// the token's own newest request for the app, so a CI log explains the failure. The token can already read its own
// requests, so this reveals nothing new.
func (h *Handler) approvalDenial(resource rbac.Resource, action rbac.Action, tokenID int64, app string) error {
	return &deployApprovalError{
		status: http.StatusForbidden,
		message: strings.TrimSuffix(forbiddenMessage(resource, action), ".") + ": " +
			h.approvalDenialReason(tokenID, app, time.Now()),
	}
}

func (h *Handler) approvalDenialReason(tokenID int64, app string, now time.Time) string {
	requests, err := h.database.ListDeployApprovalRequests(db.DeployApprovalRequestListOptions{
		TokenID: tokenID,
		App:     app,
		Limit:   1,
	})
	if err != nil {
		gtwlog.Errorf("proxy: deploy approval lookup for a denial message failed: %v", err)
		return fmt.Sprintf("this API token has no usable deploy approval for %s", app)
	}
	if len(requests) == 0 {
		return fmt.Sprintf("this API token has no deploy approval request for %s "+
			"(request one with rack-gateway deploy-approval request)", app)
	}
	newest := requests[0]
	return fmt.Sprintf("this API token has no usable deploy approval for %s: its newest request, %s (commit %s), %s",
		app, newest.PublicID, shortCommit(newest.GitCommitHash), describeApprovalState(newest, now))
}

// describeApprovalState says why a deploy approval request doesn't allow the request.
func describeApprovalState(req *db.DeployApprovalRequest, now time.Time) string {
	switch req.Status {
	case db.DeployApprovalRequestStatusPending:
		return "is waiting for approval"
	case db.DeployApprovalRequestStatusRejected:
		return "was rejected"
	case db.DeployApprovalRequestStatusDeployed:
		return "has already been deployed"
	case db.DeployApprovalRequestStatusExpired:
		if req.ApprovalExpiresAt != nil {
			return "expired at " + req.ApprovalExpiresAt.UTC().Format(time.RFC3339)
		}
		return "expired before it was approved"
	case db.DeployApprovalRequestStatusApproved:
		if req.ApprovalExpiresAt != nil && now.After(*req.ApprovalExpiresAt) {
			return "was approved, but its approval window ended at " + req.ApprovalExpiresAt.UTC().Format(time.RFC3339)
		}
		return "is approved, but this request isn't part of its deploy (a different upload, build, release or command)"
	default:
		return "is " + req.Status
	}
}
