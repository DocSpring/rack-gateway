package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// deployApprovalTracker tracks the active deploy approval for a request
type deployApprovalTracker struct {
	request   *db.DeployApprovalRequest
	tokenID   int64
	app       string
	releaseID string
}

// deployApprovalError represents an error during deploy approval evaluation
type deployApprovalError struct {
	status  int
	message string
}

func (e *deployApprovalError) Error() string { return e.message }

const deployApprovalContextKey deployApprovalContextKeyType = "deployApproval"

type deployApprovalContextKeyType string

// hasAPITokenPermission checks if an API token has the required permission
func (h *Handler) hasAPITokenPermission(authUser *auth.AuthUser, resource rbac.Resource, action rbac.Action) bool {
	// API tokens must have a TokenID
	if authUser.TokenID == nil {
		return false
	}

	// Use RBAC manager to check permissions, which handles deploy_with_approval logic
	allowed, err := h.rbacManager.EnforceForAPIToken(*authUser.TokenID, rbac.ScopeConvox, resource, action)
	if err != nil {
		// Error checking permission, deny access
		return false
	}
	return allowed
}

// tokenHasPermission checks if a token has a specific permission in its permissions list
func tokenHasPermission(perms []string, target string) bool {
	for _, perm := range perms {
		if perm == target {
			return true
		}
	}
	return false
}

// evaluateAPITokenPermission evaluates whether an API token has permission for the request.
// Returns (allowed, approvalTracker, error). If deploy_with_approval grants access, it returns
// the approval tracker in the context for downstream validation.
func (h *Handler) evaluateAPITokenPermission(r *http.Request, authUser *auth.AuthUser, rack config.RackConfig, resource rbac.Resource, action rbac.Action) (bool, *deployApprovalTracker, error) {
	if authUser == nil || authUser.TokenID == nil {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
	}

	// Check if token has direct permission (no approval required)
	if h.hasAPITokenPermission(authUser, resource, action) {
		return true, nil, nil
	}

	// If token doesn't have direct permission, check if deploy_with_approval can grant it
	// deploy_with_approval only grants access to specific deployment actions when an approval exists
	isApprovalGatedAction := (resource == rbac.ResourceObject && action == rbac.ActionCreate) ||
		(resource == rbac.ResourceBuild && action == rbac.ActionCreate) ||
		(resource == rbac.ResourceRelease && (action == rbac.ActionCreate || action == rbac.ActionPromote)) ||
		(resource == rbac.ResourceProcess && (action == rbac.ActionStart || action == rbac.ActionExec || action == rbac.ActionTerminate))

	if !isApprovalGatedAction {
		// This action cannot be granted via deploy_with_approval
		return false, nil, nil
	}

	// Check if token has deploy_with_approval permission
	hasDeployWithApproval := tokenHasPermission(authUser.Permissions, rbac.Convox(rbac.ResourceDeploy, rbac.ActionDeployWithApproval))
	if !hasDeployWithApproval {
		return false, nil, nil
	}

	// If deploy approvals are disabled, allow the action
	if h.config != nil && h.config.DeployApprovalsDisabled {
		return true, nil, nil
	}

	if h.database == nil {
		return false, nil, fmt.Errorf("database unavailable for deploy approvals")
	}

	// Extract app from URL path (e.g., /apps/{app}/releases/RXXX/promote or /apps/{app}/processes/PID/exec)
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		return false, nil, &deployApprovalError{status: http.StatusBadRequest, message: "app not found in deploy approval request"}
	}

	var req *db.DeployApprovalRequest
	var err error

	// For object:create and process actions, look up approval by token+app (any active approval for the app)
	// For release actions, require specific release approval
	if resource == rbac.ResourceObject && action == rbac.ActionCreate {
		req, err = h.database.ActiveDeployApprovalRequestByTokenAndApp(*authUser.TokenID, app)
		if err != nil {
			if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
				return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
			}
			return false, nil, err
		}
		if req == nil {
			return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
		}
	} else if resource == rbac.ResourceProcess && (action == rbac.ActionStart || action == rbac.ActionExec || action == rbac.ActionTerminate) {
		req, err = h.database.ActiveDeployApprovalRequestByTokenAndApp(*authUser.TokenID, app)
		if err != nil {
			if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
				return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
			}
			return false, nil, err
		}
		if req == nil {
			return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
		}
	} else {
		// Release promote requires specific release approval
		releaseID := extractReleaseIDFromPath(r.URL.Path)
		if releaseID == "" {
			return false, nil, &deployApprovalError{status: http.StatusBadRequest, message: "release_id not found in request"}
		}

		req, err = h.database.ActiveDeployApprovalRequestByTokenAndRelease(*authUser.TokenID, app, releaseID)
		if err != nil {
			if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
				return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
			}
			return false, nil, err
		}

		if req == nil {
			return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
		}
	}

	// Check if approval is expired
	if req.ApprovalExpiresAt != nil && time.Now().After(*req.ApprovalExpiresAt) {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
	}

	// Check if approval is in approved status
	if req.Status != db.DeployApprovalRequestStatusApproved {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
	}

	tracker := &deployApprovalTracker{
		request:   req,
		tokenID:   *authUser.TokenID,
		app:       app,
		releaseID: req.ReleaseID, // Use the release ID from the approval request
	}

	return true, tracker, nil
}

// getDeployApprovalTracker retrieves the deploy approval tracker from the request context
func getDeployApprovalTracker(ctx context.Context) *deployApprovalTracker {
	if ctx == nil {
		return nil
	}
	val := ctx.Value(deployApprovalContextKey)
	if tracker, ok := val.(*deployApprovalTracker); ok {
		return tracker
	}
	return nil
}
