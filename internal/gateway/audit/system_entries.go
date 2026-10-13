package audit

import (
	"encoding/json"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// SystemActor is the audit actor for changes the gateway makes by itself, such as expiring deploy approvals.
const SystemActor = "system"

// systemEntry is a successful change the gateway made by itself.
func systemEntry(resourceType rbac.Resource, verb, resource string, details map[string]interface{}) *db.AuditLog {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		detailsJSON = []byte("{}")
	}
	return &db.AuditLog{
		UserEmail:    SystemActor,
		UserName:     SystemActor,
		ActionType:   ActionTypeGateway,
		Action:       BuildAction(resourceType.String(), verb),
		ResourceType: resourceType.String(),
		Resource:     resource,
		Details:      string(detailsJSON),
		Status:       StatusSuccess,
	}
}

// DeployApprovalExpiredEntry is the audit event for a deploy approval request the expiry sweep expired.
func DeployApprovalExpiredEntry(expired db.ExpiredDeployApproval) *db.AuditLog {
	return systemEntry(rbac.ResourceDeployApprovalRequest, "expire", fmt.Sprintf("%d", expired.ID),
		map[string]interface{}{
			"public_id":       expired.PublicID,
			"app":             expired.App,
			"git_commit_hash": expired.GitCommitHash,
			"previous_status": expired.PreviousStatus,
		})
}

// SeededUserEntry is the audit event for a user the startup seed created from the seed lists (ADMIN_USERS etc.).
func SeededUserEntry(user db.SeededUser) *db.AuditLog {
	return systemEntry(rbac.ResourceUser, rbac.ActionCreate.String(), user.Email,
		map[string]interface{}{"roles": user.Roles, "source": "seed"})
}

// UserAutoLockedEntry is the audit event for a user the gateway locked by itself (after too many failed MFA
// attempts).
func UserAutoLockedEntry(user db.LockedUser) *db.AuditLog {
	return systemEntry(rbac.ResourceUser, ActionVerbLock, user.Email,
		map[string]interface{}{"reason": user.Reason, "target_user": user.Email})
}
