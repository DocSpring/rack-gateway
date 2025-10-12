package proxy

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	deny := func() (bool, *deployApprovalTracker, error) {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
	}

	// Must be an API token
	if authUser == nil || authUser.TokenID == nil {
		return deny()
	}

	// Direct permission wins
	if h.hasAPITokenPermission(authUser, resource, action) {
		return true, nil, nil
	}

	// Only certain actions are approval-gated
	isApprovalGatedAction := (resource == rbac.ResourceObject && action == rbac.ActionCreate) ||
		(resource == rbac.ResourceBuild && (action == rbac.ActionCreate || action == rbac.ActionRead)) ||
		(resource == rbac.ResourceRelease && action == rbac.ActionPromote) ||
		(resource == rbac.ResourceProcess && (action == rbac.ActionStart || action == rbac.ActionExec || action == rbac.ActionTerminate)) ||
		(resource == rbac.ResourceLog && action == rbac.ActionRead && isBuildLogPath(r.URL.Path))

	if !isApprovalGatedAction {
		// Not directly allowed and not approval-gated
		return false, nil, nil
	}

	// Caller must have deploy_with_approval permission
	if !tokenHasPermission(authUser.Permissions, rbac.Convox(rbac.ResourceDeploy, rbac.ActionDeployWithApproval)) {
		return false, nil, nil
	}

	// Check if deploy approvals are enabled (default: true)
	if h.settingsService != nil {
		enabled, err := h.settingsService.GetDeployApprovalsEnabled()
		if err != nil {
			// Log error but continue with safe default (enabled)
			log.Printf("Failed to get deploy_approvals_enabled setting: %v", err)
		} else if !enabled {
			// Deploy approvals disabled globally - allow with approval permission
			return true, nil, nil
		}
	}

	if h.database == nil {
		return false, nil, fmt.Errorf("database unavailable for deploy approvals")
	}

	// Resolve app
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		return deny()
	}

	// Lookup matching approval
	var (
		req *db.DeployApprovalRequest
		err error
	)

	switch {
	case resource == rbac.ResourceObject && action == rbac.ActionCreate:
		// Object upload - check that object_url is not already set
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			StatusFilter: "approved",
		})
		if err == nil && req != nil && req.ObjectURL != "" {
			return false, nil, &deployApprovalError{
				status:  http.StatusConflict,
				message: "an archive has already been uploaded for this deploy approval request",
			}
		}

	case resource == rbac.ResourceBuild && action == rbac.ActionCreate:
		// Build creation - check that build_id is not already set
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			StatusFilter: "approved",
		})
		if err == nil && req != nil && (req.BuildID != "" || req.ReleaseID != "") {
			return false, nil, &deployApprovalError{
				status:  http.StatusConflict,
				message: "a build has already been created for this deploy approval request",
			}
		}

	case resource == rbac.ResourceBuild && action == rbac.ActionRead:
		buildID := extractBuildIDFromPath(r.URL.Path)
		if buildID == "" {
			return deny()
		}
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			BuildID:      buildID,
			StatusFilter: "approved",
		})

	case resource == rbac.ResourceLog && action == rbac.ActionRead && isBuildLogPath(r.URL.Path):
		buildID := extractBuildIDFromPath(r.URL.Path)
		if buildID == "" {
			return deny()
		}
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			BuildID:      buildID,
			StatusFilter: "approved",
		})

	case resource == rbac.ResourceProcess && action == rbac.ActionStart:
		// Process start requires Release header with approved release
		releaseID := r.Header.Get("Release")
		if releaseID == "" {
			return deny()
		}
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			ReleaseID:    releaseID,
			StatusFilter: "approved",
		})

	case resource == rbac.ResourceProcess && (action == rbac.ActionExec || action == rbac.ActionTerminate):
		// Process exec/terminate requires the process ID to be in an approved deployment's process_ids
		processID := extractProcessIDFromPath(r.URL.Path)
		log.Printf("DEBUG: process %s - checking permission for tokenID=%d app=%s processID=%s", action, *authUser.TokenID, app, processID)
		if processID == "" {
			log.Printf("DEBUG: process %s - denied: empty processID", action)
			return deny()
		}
		lookup := db.DeployApprovalLookup{
			TokenID:      *authUser.TokenID,
			App:          app,
			ProcessID:    processID,
			StatusFilter: "approved",
		}
		log.Printf("DEBUG: process %s - looking up deploy approval: %+v", action, lookup)
		req, err = h.database.FindDeployApprovalRequest(lookup)
		log.Printf("DEBUG: process %s - lookup result: req=%v err=%v", action, req != nil, err)

	case resource == rbac.ResourceRelease && action == rbac.ActionPromote:
		releaseID := extractReleaseIDFromPath(r.URL.Path)
		if releaseID == "" {
			return deny()
		}
		req, err = h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
			TokenID:   *authUser.TokenID,
			App:       app,
			ReleaseID: releaseID,
		})
		if err == nil && req != nil {
			if req.Status == db.DeployApprovalRequestStatusDeployed {
				return false, nil, &deployApprovalError{
					status:  http.StatusConflict,
					message: "this deploy approval request has already been deployed",
				}
			}
			if req.Status != db.DeployApprovalRequestStatusApproved {
				return deny()
			}
		}

	default:
		return false, nil, fmt.Errorf("unsupported deploy approval resource/action: %s:%s", resource, action)
	}

	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			log.Printf("DEBUG: deploy approval lookup failed: not found")
			return deny()
		}
		log.Printf("DEBUG: deploy approval lookup failed: %v", err)
		return false, nil, err
	}
	if req == nil {
		log.Printf("DEBUG: deploy approval lookup returned nil request")
		return deny()
	}

	log.Printf("DEBUG: deploy approval found: id=%s status=%s process_ids=%v", req.PublicID, req.Status, req.ProcessIDs)

	// Common checks
	if req.ApprovalExpiresAt != nil && time.Now().After(*req.ApprovalExpiresAt) {
		log.Printf("DEBUG: deploy approval expired")
		return deny()
	}
	if req.Status != db.DeployApprovalRequestStatusApproved {
		log.Printf("DEBUG: deploy approval status check failed: expected=approved actual=%s", req.Status)
		return deny()
	}

	log.Printf("DEBUG: deploy approval permission granted")

	tracker := &deployApprovalTracker{
		request:   req,
		tokenID:   *authUser.TokenID,
		app:       app,
		releaseID: req.ReleaseID, // may be nil
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
