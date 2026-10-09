package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

const defaultManifestPath = "convox.yml"

// validateBuildManifestForAllUsers validates build manifests against the app's configured image patterns.
// It applies to every build that is not bound to a deploy approval (users and API tokens with direct
// build permission). Approval-bound token builds are validated by validateTokenBuildBinding instead.
func (h *Handler) validateBuildManifestForAllUsers(r *http.Request, vals url.Values) error {
	app := extractAppFromPath(r.URL.Path)
	patterns, err := h.settingsService.GetServiceImagePatterns(app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}
	if len(patterns) == 0 {
		return nil
	}

	objectURL := strings.TrimSpace(vals.Get("url"))
	if objectURL == "" {
		return fmt.Errorf("build request missing object URL")
	}
	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		return fmt.Errorf("git-sha is required when image pattern validation is configured")
	}

	policy := newImagePolicy(patterns, gitSHA, false)
	if err := h.validateBuildManifest(r.Context(), app, objectURL, manifestPathFrom(vals), policy); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}
	return nil
}

// validateTokenBuildBinding binds an API-token build to the deploy approval that authorized it:
//   - the build must use the archive that was uploaded under that approval,
//   - a git-sha, if sent, must equal the approved commit,
//   - every service image in the uploaded manifest must be tagged with the approved commit, and
//   - the app's configured image patterns (if any) must also match.
//
// The approved commit always comes from the approval record, never from the client.
func (h *Handler) validateTokenBuildBinding(r *http.Request, vals url.Values, tracker *deployApprovalTracker) error {
	approval := tracker.request
	objectURL := strings.TrimSpace(vals.Get("url"))
	if objectURL == "" || objectURL != approval.ObjectURL {
		return fmt.Errorf("build must use the archive uploaded for deploy approval %s", approval.PublicID)
	}

	commit, ok := db.NormalizeCommitSHA(approval.GitCommitHash)
	if !ok {
		return fmt.Errorf("deploy approval %s is not bound to a full git commit SHA", approval.PublicID)
	}
	if gitSHA := strings.TrimSpace(vals.Get("git-sha")); gitSHA != "" && !strings.EqualFold(gitSHA, commit) {
		return fmt.Errorf("git-sha %s does not match the approved commit %s", gitSHA, commit)
	}

	patterns, err := h.settingsService.GetServiceImagePatterns(tracker.app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}
	// The commit tag alone doesn't pin the image repository (attacker/image:<sha> would pass),
	// so approval-bound builds require patterns that name the repository.
	if len(patterns) == 0 {
		return fmt.Errorf(
			"deploy approvals for app %s require service_image_patterns to pin the image repository "+
				`(e.g. {"*": "docker\\.io/org/app:{{GIT_COMMIT}}-amd64"})`,
			tracker.app,
		)
	}

	policy := newImagePolicy(patterns, commit, true)
	if err := h.validateBuildManifest(r.Context(), tracker.app, objectURL, manifestPathFrom(vals), policy); err != nil {
		gtwlog.Warnf("deploy approval %s: build manifest rejected: %v", approval.PublicID, err)
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	gtwlog.Infof("deploy approval %s: build bound to commit %s and object %s", approval.PublicID, commit, objectURL)
	return nil
}

func manifestPathFrom(vals url.Values) string {
	if manifestPath := strings.TrimSpace(vals.Get("manifest")); manifestPath != "" {
		return manifestPath
	}
	return defaultManifestPath
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
//
// Note: releaseID may be empty initially when the build is first created. The real Convox API
// returns Build responses with an empty release field, which gets populated later when the build completes.
func (h *Handler) updateBuildApprovalTracking(r *http.Request, buildID, releaseID string) {
	if buildID == "" {
		gtwlog.Warnf("updateBuildApprovalTracking: skipping update (empty buildID)")
		return
	}

	// Use the permission-based tracker which is set by RBAC when permission is granted
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil {
		gtwlog.Errorf(
			"updateBuildApprovalTracking: NO TRACKER - BuildID will not be saved! "+
				"buildID=%s releaseID=%s",
			buildID,
			releaseID,
		)
		return
	}

	gtwlog.Infof(
		"updateBuildApprovalTracking: updating approval_id=%d public_id=%s "+
			"buildID=%s releaseID=%s",
		tracker.request.ID,
		tracker.request.PublicID,
		buildID,
		releaseID,
	)

	err := h.database.MarkDeployApprovalRequestBuildStarted(tracker.request.ID, buildID, releaseID)
	if err != nil {
		gtwlog.Errorf("updateBuildApprovalTracking: failed to update approval: %v", err)
	} else {
		gtwlog.Infof(
			"updateBuildApprovalTracking: successfully updated approval %d "+
				"buildID=%s releaseID=%s",
			tracker.request.ID,
			buildID,
			releaseID,
		)
	}
}
