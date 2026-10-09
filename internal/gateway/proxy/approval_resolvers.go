package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Approval resolvers find the deploy approval request that authorizes an approval-gated
// Convox request for an API token. Each step of a deploy is bound to the previous one:
// object upload -> build (by uploaded object URL) -> release (by build) -> processes (by release).

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
			r *http.Request,
			deny denyFunc,
			tokenID int64,
			app string,
		) (*db.DeployApprovalRequest, error) {
			return h.findApprovalForBuildCreate(r, deny, tokenID, app)
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

// findApprovalForBuildCreate selects the approval whose uploaded archive this build uses.
// A token build can only build the object that was uploaded under its own approved request.
func (h *Handler) findApprovalForBuildCreate(
	r *http.Request,
	deny denyFunc,
	tokenID int64,
	app string,
) (*db.DeployApprovalRequest, error) {
	objectURL, err := buildRequestObjectURL(r)
	if err != nil {
		return nil, err
	}
	if objectURL == "" {
		_, _, denyErr := deny()
		return nil, denyErr
	}
	req, err := h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:      tokenID,
		App:          app,
		ObjectURL:    objectURL,
		StatusFilter: "approved",
	})
	if err == nil && req != nil && (req.BuildID != "" || req.ReleaseID != "") {
		return nil, &deployApprovalError{
			status:  http.StatusConflict,
			message: "a build has already been created for this deploy approval request",
		}
	}
	return req, err
}

// buildRequestObjectURL reads the object URL from a form-encoded build request and restores the body.
func buildRequestObjectURL(r *http.Request) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read build request: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return "", &deployApprovalError{status: http.StatusBadRequest, message: "invalid build request body"}
	}
	return strings.TrimSpace(vals.Get("url")), nil
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
