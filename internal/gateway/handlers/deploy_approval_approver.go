package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// approvalWindowRequest is an approve or extend request: either opens an approval window, which runs to expiresAt.
type approvalWindowRequest struct {
	*deployApprovalStatusInput
	expiresAt    time.Time
	auditDetails map[string]string
}

// parseApprovalWindowRequest parses an approve or extend request and checks its approver may open the window.
func (h *AdminHandler) parseApprovalWindowRequest(c *gin.Context) (*approvalWindowRequest, bool) {
	input, ok := h.parseDeployApprovalStatusUpdateRequest(c)
	if !ok {
		return nil, false
	}
	differentApproverChecked, ok := h.requireDifferentApprover(c, input)
	if !ok {
		return nil, false
	}
	expiresAt := h.getDeployApprovalExpiry()
	details := map[string]string{"expires_at": expiresAt.UTC().Format(time.RFC3339)}
	if differentApproverChecked {
		details[differentApproverAuditKey] = "true"
	}
	return &approvalWindowRequest{deployApprovalStatusInput: input, expiresAt: expiresAt, auditDetails: details}, true
}

// differentApproverAuditKey marks an approval or extension that deploy_approval_require_different_approver checked.
const differentApproverAuditKey = "different_approver_required"

// requireDifferentApprover refuses an approval or extension by the person who asked for the deploy, when
// deploy_approval_require_different_approver is on: the user who created the request, or the owner of the API token
// it's for. A setting it can't read refuses the request. checked says the setting applied, for the audit entry.
func (h *AdminHandler) requireDifferentApprover(c *gin.Context, input *deployApprovalStatusInput) (checked, ok bool) {
	if h.settingsService == nil {
		return false, true
	}
	required, err := h.settingsService.GetDeployApprovalRequireDifferentApprover()
	if err != nil {
		gtwlog.Errorf("deploy approvals: failed to read %s: %v",
			settings.KeyDeployApprovalRequireDifferentApprover, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read deploy approval settings"})
		return false, false
	}
	if !required {
		return false, true
	}
	record, ok := loadDeployApprovalRequest(c, h.database, input.publicID)
	if !ok {
		return false, false
	}
	if reason := ownDeployApprovalReason(record, input.approver.ID); reason != "" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "this gateway requires a different person to approve a deploy: " + reason,
		})
		return false, false
	}
	return true, true
}

// ownDeployApprovalReason says why a user counts as the requester of a deploy approval request, or "" if they don't.
func ownDeployApprovalReason(record *db.DeployApprovalRequest, userID int64) string {
	switch {
	case sameID(record.CreatedByUserID, userID):
		return "you created this request"
	case sameID(record.TargetUserID, userID):
		return "you own the API token this request is for"
	default:
		return ""
	}
}
