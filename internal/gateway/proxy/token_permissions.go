package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
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
func (h *Handler) hasAPITokenPermission(authUser *auth.User, resource rbac.Resource, action rbac.Action) bool {
	// API tokens must have a TokenID
	if authUser.TokenID == nil {
		return false
	}

	allowed, err := h.rbacManager.Authorize(authUser.Principal(), rbac.Convox(resource, action))
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
func (h *Handler) evaluateAPITokenPermission(
	r *http.Request,
	authUser *auth.User,
	resource rbac.Resource,
	action rbac.Action,
) (bool, *deployApprovalTracker, error) {
	deny := func() (bool, *deployApprovalTracker, error) {
		return false, nil, &deployApprovalError{
			status:  http.StatusForbidden,
			message: forbiddenMessage(resource, action),
		}
	}

	if !h.isValidAPIToken(authUser) {
		return deny()
	}

	if h.hasAPITokenPermission(authUser, resource, action) {
		return true, nil, nil
	}

	if !isApprovalGated(resource, action, r.URL.Path) {
		return false, nil, nil
	}

	if !callerHasDeployWithApproval(authUser) || !h.tokenOwnerCan(authUser, resource, action) {
		return false, nil, nil
	}

	if approvalsGloballyDisabled(h) {
		return true, nil, nil
	}

	if h.database == nil {
		return false, nil, fmt.Errorf("database unavailable for deploy approvals")
	}

	app, ok := h.resolveAppOrDeny(r.URL.Path, deny)
	if !ok {
		return false, nil, nil
	}

	req, err := h.findDeployApprovalForResource(r, deny, *authUser.TokenID, app, resource, action)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "deploy approval lookup failed: not found")
			return deny()
		}
		gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "deploy approval lookup failed: %v", err)
		return false, nil, err
	}
	if ok := commonApprovalChecks(req, deny); !ok {
		return false, nil, nil
	}

	tracker := &deployApprovalTracker{
		request:   req,
		tokenID:   *authUser.TokenID,
		app:       app,
		releaseID: req.ReleaseID,
	}
	gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "deploy approval permission granted")
	return true, tracker, nil
}

func (_ *Handler) isValidAPIToken(authUser *auth.User) bool {
	return authUser != nil && authUser.TokenID != nil
}

// approvalGatedActions maps resources to their approval-gated actions
var approvalGatedActions = map[rbac.Resource][]rbac.Action{
	rbac.ResourceObject:  {rbac.ActionCreate},
	rbac.ResourceBuild:   {rbac.ActionCreate, rbac.ActionRead},
	rbac.ResourceRelease: {rbac.ActionPromote, rbac.ActionRead},
	rbac.ResourceProcess: {rbac.ActionStart, rbac.ActionExec, rbac.ActionTerminate},
}

func isApprovalGated(resource rbac.Resource, action rbac.Action, path string) bool {
	// Special case: log reads are only gated for build logs
	if resource == rbac.ResourceLog && action == rbac.ActionRead && isBuildLogPath(path) {
		return true
	}

	// Check if action is in the list of gated actions for this resource
	actions, ok := approvalGatedActions[resource]
	if !ok {
		return false
	}
	for _, gatedAction := range actions {
		if action == gatedAction {
			return true
		}
	}
	return false
}

// tokenOwnerCan reports whether the token owner's roles allow the action: approval never exceeds the owner.
func (h *Handler) tokenOwnerCan(authUser *auth.User, resource rbac.Resource, action rbac.Action) bool {
	allowed, err := h.rbacManager.Authorize(rbac.UserPrincipal(authUser.DBUser), rbac.Convox(resource, action))
	return err == nil && allowed
}

func callerHasDeployWithApproval(authUser *auth.User) bool {
	return tokenHasPermission(authUser.Permissions, rbac.Convox(rbac.ResourceDeploy, rbac.ActionDeployWithApproval))
}

func approvalsGloballyDisabled(h *Handler) bool {
	if h.settingsService == nil {
		return false
	}
	enabled, err := h.settingsService.GetDeployApprovalsEnabled()
	if err != nil {
		gtwlog.Warnf("Failed to get deploy_approvals_enabled setting: %v", err)
		return false
	}
	return !enabled
}

func (_ *Handler) resolveAppOrDeny(path string, deny denyFunc) (string, bool) {
	app := extractAppFromPath(path)
	if app == "" {
		_, _, _ = deny()
		return "", false
	}
	return app, true
}

func commonApprovalChecks(req *db.DeployApprovalRequest, deny denyFunc) bool {
	if req == nil {
		gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "deploy approval lookup returned nil request")
		_, _, _ = deny()
		return false
	}
	gtwlog.DebugTopicf(
		gtwlog.TopicDeployApproval,
		"deploy approval found: id=%s status=%s process_ids=%v",
		req.PublicID, req.Status, req.ProcessIDs,
	)
	if req.ApprovalExpiresAt != nil && time.Now().After(*req.ApprovalExpiresAt) {
		gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "deploy approval expired")
		_, _, _ = deny()
		return false
	}
	if req.Status != db.DeployApprovalRequestStatusApproved {
		gtwlog.DebugTopicf(
			gtwlog.TopicDeployApproval,
			"deploy approval status check failed: expected=approved actual=%s",
			req.Status,
		)
		_, _, _ = deny()
		return false
	}
	return true
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
