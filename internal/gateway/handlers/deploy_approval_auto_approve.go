package handlers

import (
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// autoApprovalNote is the approval note on a request the gateway approved by itself.
const autoApprovalNote = "Approved automatically: deploy_approval_auto_approve is on"

// autoApprove approves a new request straight away when deploy_approval_auto_approve is on: a gateway for a rack
// where the CI pipeline's own approval step decides when a deploy runs. The approval works like a person's (the
// same window, binding and approved commands), with no approving user, and it never approves a CircleCI job. It
// doesn't apply while deploy_approval_require_different_approver is on. When a setting can't be read or the
// approval fails, the request stays pending for a person to approve, and ok is false.
func (h *APIHandler) autoApprove(c *gin.Context, record *db.DeployApprovalRequest) (*db.DeployApprovalRequest, bool) {
	if !h.autoApprovalApplies() {
		return nil, false
	}

	expiresAt, err := deployApprovalExpiry(h.settingsService)
	if err != nil {
		reportAutoApprovalFailure(record, err)
		return nil, false
	}

	approved, err := h.database.AutoApproveDeployApprovalRequest(record.ID, expiresAt, autoApprovalNote)
	if err != nil {
		reportAutoApprovalFailure(record, err)
		return nil, false
	}

	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), audit.DeployApprovalAutoApprovedEntry(approved))
	return approved, true
}

// autoApprovalApplies reports whether new requests are approved as they're created. A setting it can't read
// counts as off, so the request waits for a person.
func (h *APIHandler) autoApprovalApplies() bool {
	if h.settingsService == nil {
		return false
	}

	autoApproval, err := h.settingsService.GetDeployApprovalAutoApproval()
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to read %s and %s: %v", settings.KeyDeployApprovalAutoApprove,
			settings.KeyDeployApprovalRequireDifferentApprover, err)
		return false
	}
	if !autoApproval.Enabled {
		return false
	}
	if autoApproval.RequireDifferentApprover {
		gtwlog.Warnf("deploy approvals: %s is on, so %s doesn't apply; the request waits for an approver",
			settings.KeyDeployApprovalRequireDifferentApprover, settings.KeyDeployApprovalAutoApprove)
		return false
	}
	return true
}

func reportAutoApprovalFailure(record *db.DeployApprovalRequest, err error) {
	err = fmt.Errorf("deploy approvals: auto-approving request %s failed, so it waits for an approver: %w",
		record.PublicID, err)
	gtwlog.Errorf("%v", err)
	sentry.CaptureException(err)
}

// deployApprovalExpiry is when an approval or extension made now ends: deploy_approval_window_minutes from now. A
// setting it can't read is an error, never the default window, which may be longer than the one configured.
func deployApprovalExpiry(settingsService *settings.Service) (time.Time, error) {
	windowMinutes := settings.DefaultDeployApprovalWindowMinutes
	if settingsService != nil {
		minutes, err := settingsService.GetDeployApprovalWindowMinutes()
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to read %s: %w", settings.KeyDeployApprovalWindowMinutes, err)
		}
		windowMinutes = minutes
	}
	return time.Now().Add(time.Duration(windowMinutes) * time.Minute), nil
}
