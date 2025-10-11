package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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
		return fmt.Errorf("invalid build request body")
	}

	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		// No git-sha in request, skip deploy approval validation
		return nil
	}

	// Check if there's an active approved deployment for this token and git commit
	approval, err := h.database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitSHA,
		StatusFilter:  "approved",
	})
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
		}
		return fmt.Errorf("failed to check deploy approval: %w", err)
	}

	if approval == nil {
		return fmt.Errorf("deployment approval required for git commit %s", gitSHA)
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

	return nil
}

// updateObjectURLApprovalTracking updates the deploy approval request with object_url
// after a successful object upload
func (h *Handler) updateObjectURLApprovalTracking(r *http.Request, objectURL string) error {
	val := r.Context().Value(deployApprovalContextKey)
	tracker, ok := val.(*deployApprovalTracker)
	if !ok || tracker == nil || tracker.request == nil {
		return nil
	}

	if objectURL == "" {
		return nil
	}

	err := h.database.UpdateDeployApprovalRequestObjectURL(tracker.request.ID, objectURL)
	if err != nil {
		return fmt.Errorf("failed to track object URL for deployment approval: %w", err)
	}
	return nil
}

// updateBuildApprovalTracking updates the deploy approval request with build_id and release_id
// after a successful build creation
func (h *Handler) updateBuildApprovalTracking(r *http.Request, buildID, releaseID string) {
	ctx, ok := r.Context().Value(buildApprovalContextKey{}).(*buildApprovalContext)
	if !ok || ctx == nil {
		return
	}

	if buildID == "" || releaseID == "" {
		return
	}

	err := h.database.UpdateDeployApprovalRequestBuild(ctx.approvalID, buildID, releaseID)
	if err != nil {
		// Log error but don't fail the request - build already succeeded
		fmt.Printf("WARNING: failed to update deploy approval tracking: %v\n", err)
	}
}
