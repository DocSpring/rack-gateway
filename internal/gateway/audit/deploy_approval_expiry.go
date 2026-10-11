package audit

import (
	"encoding/json"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// SystemActor is the audit actor for changes the gateway makes by itself, such as expiring deploy approvals.
const SystemActor = "system"

// DeployApprovalExpiredEntry is the audit event for a deploy approval request the expiry sweep expired.
func DeployApprovalExpiredEntry(expired db.ExpiredDeployApproval) *db.AuditLog {
	details, err := json.Marshal(map[string]string{
		"public_id":       expired.PublicID,
		"app":             expired.App,
		"git_commit_hash": expired.GitCommitHash,
		"previous_status": expired.PreviousStatus,
	})
	if err != nil {
		details = []byte("{}")
	}
	return &db.AuditLog{
		UserEmail:    SystemActor,
		UserName:     SystemActor,
		ActionType:   ActionTypeGateway,
		Action:       BuildAction(rbac.ResourceDeployApprovalRequest.String(), "expire"),
		ResourceType: rbac.ResourceDeployApprovalRequest.String(),
		Resource:     fmt.Sprintf("%d", expired.ID),
		Details:      string(details),
		Status:       StatusSuccess,
	}
}
