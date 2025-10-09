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

// validateBuildRequest validates build requests against active deploy approvals.
// This is only called for API token requests.
// It verifies that:
// 1. The git-sha in the request matches an approved deploy request
// 2. The manifest in the tarball matches the required image tag pattern
// 3. Stores the context for updating the approval after successful build
func (h *Handler) validateBuildRequest(r *http.Request, bodyBytes []byte, tokenID int64) error {
	// Parse request body (form-encoded: git-sha=abc123&url=object://app/tmp/file.tgz&manifest=convox.yml&...)
	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return fmt.Errorf("invalid build request body")
	}

	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		// No git-sha in request, skip validation
		return nil
	}

	// Check if there's an active approved deployment for this token and git commit
	approval, err := h.database.ActiveDeployApprovalRequestByTokenAndCommit(tokenID, gitSHA)
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

	// Check if manifest validation is required for this app
	patterns, err := h.database.GetAppImagePatterns()
	if err != nil {
		return fmt.Errorf("failed to get app image patterns: %w", err)
	}

	if patternTemplate, hasPattern := patterns[app]; hasPattern {
		// Manifest validation is required - extract and validate the manifest
		objectURL := strings.TrimSpace(vals.Get("url"))
		manifestPath := strings.TrimSpace(vals.Get("manifest"))
		if manifestPath == "" {
			manifestPath = "convox.yml" // Default manifest name
		}

		if objectURL == "" {
			return fmt.Errorf("build request missing object URL")
		}

		// Validate the manifest from the tarball
		if err := h.validateBuildManifest(r.Context(), app, objectURL, manifestPath, patternTemplate, approval.GitCommitHash); err != nil {
			return fmt.Errorf("manifest validation failed: %w", err)
		}
	}

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
func (h *Handler) updateObjectURLApprovalTracking(r *http.Request, objectURL string) {
	val := r.Context().Value(deployApprovalContextKey)
	tracker, ok := val.(*deployApprovalTracker)
	if !ok || tracker == nil || tracker.request == nil {
		return
	}

	if objectURL == "" {
		return
	}

	err := h.database.UpdateDeployApprovalRequestObjectURL(tracker.request.ID, objectURL)
	if err != nil {
		// Log error but don't fail the request - upload already succeeded
		fmt.Printf("WARNING: failed to update deploy approval object URL tracking: %v\n", err)
	}
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
