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

	if !callerHasDeployWithApproval(authUser) {
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

type denyFunc func() (bool, *deployApprovalTracker, error)

func (h *Handler) findDeployApprovalForResource(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
	resource rbac.Resource,
	action rbac.Action,
) (*db.DeployApprovalRequest, error) {
	if resolver := h.approvalResolverFor(resource, action, r.URL.Path); resolver != nil {
		return resolver(r, deny, tokenID, app)
	}
	return nil, fmt.Errorf("unsupported deploy approval resource/action: %s:%s", resource, action)
}

type approvalResolver func(*http.Request, denyFunc, int64, string) (*db.DeployApprovalRequest, error)

func (h *Handler) approvalResolverFor(resource rbac.Resource, action rbac.Action, path string) approvalResolver {
	key := resource.String() + ":" + action.String()
	table := map[string]approvalResolver{
		rbac.ResourceObject.String() + ":" + rbac.ActionCreate.String(): func(
			_ *http.Request,
			_ denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForObjectCreate(tokenID, app)
		},
		rbac.ResourceBuild.String() + ":" + rbac.ActionCreate.String(): func(
			_ *http.Request,
			_ denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForBuildCreate(tokenID, app)
		},
		rbac.ResourceBuild.String() + ":" + rbac.ActionRead.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForBuildRead(r, deny, tokenID, app)
		},
		rbac.ResourceProcess.String() + ":" + rbac.ActionStart.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForProcessStart(r, deny, tokenID, app)
		},
		rbac.ResourceProcess.String() + ":" + rbac.ActionExec.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForProcessAction(r, deny, tokenID, app, rbac.ActionExec)
		},
		rbac.ResourceProcess.String() + ":" + rbac.ActionTerminate.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForProcessAction(r, deny, tokenID, app, rbac.ActionTerminate)
		},
		rbac.ResourceRelease.String() + ":" + rbac.ActionRead.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForReleaseRead(r, deny, tokenID, app)
		},
		rbac.ResourceRelease.String() + ":" + rbac.ActionPromote.String(): func(
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForReleasePromote(r, deny, tokenID, app)
		},
	}
	// Special case: logs read only applies when path is build logs
	if resource == rbac.ResourceLog && action == rbac.ActionRead && isBuildLogPath(path) {
		return table[rbac.ResourceBuild.String()+":"+rbac.ActionRead.String()]
	}
	return table[key]
}

func (h *Handler) findApprovalForObjectCreate(
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	req, err := h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		StatusFilter: "approved",
	})
	// Fail if object_url is already set - this means an object was already uploaded for this approval.
	// Object uploads should only happen once per approval (unlike builds, which can fail and retry).
	if err == nil && req != nil && req.ObjectURL != "" {
		return nil, &deployApprovalError{
			status:  http.StatusConflict,
			message: "an archive has already been uploaded for this deploy approval request",
		}
	}
	return req, err
}

func (h *Handler) findApprovalForBuildCreate(
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	req, err := h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		StatusFilter: "approved",
	})
	if err == nil && req != nil {
		if req.BuildID != "" || req.ReleaseID != "" {
			return nil, &deployApprovalError{
				status:  http.StatusConflict,
				message: "a build has already been created for this deploy approval request",
			}
		}
		// Check if object_url is already set AND build exists - if so, this is a duplicate upload attempt.
		// If object_url is set but build hasn't been created yet, that's OK - it's the normal flow.
		if req.ObjectURL != "" && (req.BuildID != "" || req.ReleaseID != "") {
			return nil, &deployApprovalError{
				status:  http.StatusConflict,
				message: "an archive has already been uploaded for this deploy approval request",
			}
		}
	}
	return req, err
}

func (h *Handler) findApprovalForBuildRead(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	buildID := extractBuildIDFromPath(r.URL.Path)
	if buildID == "" {
		_, _, err := deny()
		return nil, err
	}
	return h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		BuildID:      buildID,
		StatusFilter: "approved",
	})
}

func (h *Handler) findApprovalForProcessStart(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	releaseID := r.Header.Get("Release")
	if releaseID == "" {
		_, _, err := deny()
		return nil, err
	}
	return h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		ReleaseID:    releaseID,
		StatusFilter: "approved",
	})
}

func (h *Handler) findApprovalForProcessAction(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
	action rbac.Action,
) (*db.DeployApprovalRequest, error) {
	processID := extractProcessIDFromPath(r.URL.Path)
	gtwlog.DebugTopicf(gtwlog.TopicDeployApproval,
		"process %s - checking permission for tokenID=%d app=%s processID=%s",
		action,
		tokenID,
		app,
		processID,
	)
	if processID == "" {
		gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "process %s - denied: empty processID", action)
		_, _, err := deny()
		return nil, err
	}
	lookup := db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		ProcessID:    processID,
		StatusFilter: "approved",
	}
	gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "process %s - looking up deploy approval: %+v", action, lookup)
	req, err := h.database.FindDeployApprovalRequest(lookup)
	gtwlog.DebugTopicf(gtwlog.TopicDeployApproval, "process %s - lookup result: req=%v err=%v", action, req != nil, err)
	return req, err
}

func (h *Handler) findApprovalForReleaseRead(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	// Use the same logic as promote - extract release ID and look up approval
	return h.findApprovalForRelease(r, deny, tokenID, app, false)
}

func (h *Handler) findApprovalForReleasePromote(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	// Promote needs to check for already-deployed status
	return h.findApprovalForRelease(r, deny, tokenID, app, true)
}

func (h *Handler) findApprovalForRelease(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
	checkDeployed bool,
) (*db.DeployApprovalRequest, error) {
	releaseID := extractReleaseIDFromPath(r.URL.Path)
	if releaseID == "" {
		_, _, err := deny()
		return nil, err
	}

	// For promote, we need to check if already deployed, so we can't filter by status
	// For read, we can filter by approved status for efficiency
	lookup := db.DeployApprovalLookup{
		TokenID:   tokenID,
		App:       app,
		ReleaseID: releaseID,
	}
	if !checkDeployed {
		lookup.StatusFilter = "approved"
	}

	req, err := h.database.FindDeployApprovalRequest(lookup)
	if err == nil && req != nil {
		if checkDeployed && req.Status == db.DeployApprovalRequestStatusDeployed {
			return nil, &deployApprovalError{
				status:  http.StatusConflict,
				message: "this deploy approval request has already been deployed",
			}
		}
		if req.Status != db.DeployApprovalRequestStatusApproved {
			_, _, denyErr := deny()
			return nil, denyErr
		}
	}
	return req, err
}
