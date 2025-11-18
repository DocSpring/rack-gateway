package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

type buildApprovalContext struct {
	approvalID int64
	gitCommit  string
	app        string
}

type buildApprovalContextKey struct{}

// validateBuildManifestForAllUsers validates build manifests against configured image patterns.
// This applies to ALL users (admin, deployer, API tokens) and enforces security policies.
func (h *Handler) validateBuildManifestForAllUsers(r *http.Request, bodyBytes []byte) error {
	// Parse request body (form-encoded: url=object://app/tmp/file.tgz&manifest=convox.yml&...)
	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return fmt.Errorf("invalid build request body")
	}

	// Get app name from path
	app := extractAppFromPath(r.URL.Path)

	// Check if manifest validation is required for this app
	patterns, err := h.settingsService.GetServiceImagePatterns(app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}

	if len(patterns) > 0 {
		// Manifest validation is required - extract and validate the manifest
		objectURL := strings.TrimSpace(vals.Get("url"))
		manifestPath := strings.TrimSpace(vals.Get("manifest"))
		if manifestPath == "" {
			manifestPath = "convox.yml" // Default manifest name
		}

		if objectURL == "" {
			return fmt.Errorf("build request missing object URL")
		}

		// Get git commit for validation
		gitSHA := strings.TrimSpace(vals.Get("git-sha"))
		if gitSHA == "" {
			return fmt.Errorf("git-sha is required when image pattern validation is configured")
		}

		// Validate the manifest from the tarball
		if err := h.validateBuildManifest(r.Context(), app, objectURL, manifestPath, patterns, gitSHA); err != nil {
			return fmt.Errorf("manifest validation failed: %w", err)
		}
	}

	return nil
}

// validateBuildRequestForAPIToken validates build requests for API tokens with deploy approvals.
// This is only called for API token requests and handles deploy approval tracking.
func (h *Handler) validateBuildRequestForAPIToken(r *http.Request, bodyBytes []byte, tokenID int64) error {
	// Parse request body (form-encoded: git-sha=abc123&url=object://app/tmp/file.tgz&manifest=convox.yml&...)
	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		gtwlog.Errorf("validateBuildRequestForAPIToken: failed to parse request body: %v", err)
		return fmt.Errorf("invalid build request body")
	}

	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		// No git-sha in request, skip git-sha validation
		// RBAC permission check will still run separately
		gtwlog.Infof(
			"validateBuildRequestForAPIToken: no git-sha, skipping git-sha validation (tokenID=%d)",
			tokenID,
		)
		return nil
	}

	gtwlog.Infof(
		"validateBuildRequestForAPIToken: looking up approval for tokenID=%d git-sha=%s",
		tokenID,
		gitSHA,
	)

	// Check if there's an active approved deployment for this token and git commit
	approval, err := h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitSHA,
		StatusFilter:  "approved",
	})
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			gtwlog.Warnf(
				"validateBuildRequestForAPIToken: no approved deployment (tokenID=%d git-sha=%s)",
				tokenID,
				gitSHA,
			)
			return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
		}
		gtwlog.Errorf("validateBuildRequestForAPIToken: database error looking up approval: %v", err)
		return fmt.Errorf("failed to check deploy approval: %w", err)
	}

	if approval == nil {
		gtwlog.Warnf(
			"validateBuildRequestForAPIToken: approval lookup returned nil (tokenID=%d git-sha=%s)",
			tokenID,
			gitSHA,
		)
		return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
	}

	gtwlog.Infof(
		"validateBuildRequestForAPIToken: found approval id=%d public_id=%s for git-sha=%s",
		approval.ID,
		approval.PublicID,
		gitSHA,
	)

	// Check for duplicate: only fail if object_url is set AND build already exists.
	// The normal flow is: object upload (sets object_url) → build creation (uses same approval).
	// If object_url is set but build hasn't been created yet, that's OK - it's the normal flow.
	// This must happen BEFORE manifest validation to catch true duplicates even if manifest is invalid.
	if approval.ObjectURL != "" && (approval.BuildID != "" || approval.ReleaseID != "") {
		gtwlog.Warnf(
			"validateBuildRequestForAPIToken: duplicate build (approval id=%d object_url=%s build_id=%s release_id=%s)",
			approval.ID,
			approval.ObjectURL,
			approval.BuildID,
			approval.ReleaseID,
		)
		return fmt.Errorf("an archive has already been uploaded for this deploy approval request")
	}

	// Get app name from path
	app := extractAppFromPath(r.URL.Path)

	// Store approval in context so we can update it after successful build
	ctx := context.WithValue(r.Context(), buildApprovalContextKey{}, &buildApprovalContext{
		approvalID: approval.ID,
		gitCommit:  gitSHA,
		app:        app,
	})
	*r = *r.WithContext(ctx)

	gtwlog.Infof(
		"validateBuildRequestForAPIToken: stored buildApprovalContext (approval_id=%d git-sha=%s)",
		approval.ID,
		gitSHA,
	)

	return nil
}

// updateObjectURLApprovalTracking updates the deploy approval request with object_url
// after a successful object upload
func (h *Handler) updateObjectURLApprovalTracking(r *http.Request, objectURL string) error {
	if objectURL == "" {
		gtwlog.Warnf("updateObjectURLApprovalTracking: skipping (empty objectURL)")
		return nil
	}

	val := r.Context().Value(deployApprovalContextKey)
	tracker, ok := val.(*deployApprovalTracker)
	if !ok || tracker == nil || tracker.request == nil {
		gtwlog.Errorf(
			"updateObjectURLApprovalTracking: NO TRACKER - object_url will not be saved! objectURL=%s",
			objectURL,
		)
		return nil
	}

	gtwlog.Infof(
		"updateObjectURLApprovalTracking: updating approval_id=%d with objectURL=%s",
		tracker.request.ID,
		objectURL,
	)

	err := h.database.UpdateDeployApprovalRequestObjectURL(tracker.request.ID, objectURL)
	if err != nil {
		gtwlog.Errorf("updateObjectURLApprovalTracking: failed to update database: %v", err)
		return fmt.Errorf("failed to track object URL for deployment approval: %w", err)
	}

	gtwlog.Infof("updateObjectURLApprovalTracking: successfully saved object_url to approval %d", tracker.request.ID)
	return nil
}

// updateBuildApprovalTracking updates the deploy approval request with build_id and release_id
// after a successful build creation.
//
// IMPORTANT: This uses the permission-based tracker (deployApprovalContextKey) which is THE MAIN
// approval tracker set by RBAC when permission is granted. The git-SHA validation is an additional
// security layer on top - when present, both must pass, but the permission tracker is always authoritative.
func (h *Handler) updateBuildApprovalTracking(r *http.Request, buildID, releaseID string) {
	if buildID == "" || releaseID == "" {
		gtwlog.Warnf("updateBuildApprovalTracking: skipping update (empty buildID or releaseID)")
		return
	}

	// Use the permission-based tracker which is set by RBAC when permission is granted
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil {
		gtwlog.Errorf(
			"updateBuildApprovalTracking: NO TRACKER - BuildID will not be saved! buildID=%s releaseID=%s",
			buildID,
			releaseID,
		)
		return
	}

	gtwlog.Infof("updateBuildApprovalTracking: updating approval_id=%d public_id=%s with buildID=%s releaseID=%s",
		tracker.request.ID, tracker.request.PublicID, buildID, releaseID)

	err := h.database.MarkDeployApprovalRequestBuildStarted(tracker.request.ID, buildID, releaseID)
	if err != nil {
		gtwlog.Errorf("updateBuildApprovalTracking: failed to update approval: %v", err)
	} else {
		gtwlog.Infof("updateBuildApprovalTracking: successfully updated approval %d with buildID=%s releaseID=%s",
			tracker.request.ID, buildID, releaseID)
	}
}
