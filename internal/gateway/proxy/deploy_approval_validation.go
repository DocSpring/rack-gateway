package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

const defaultManifestPath = "convox.yml"

// validateBuildManifestForAllUsers validates build manifests against the app's configured image patterns.
// It applies to every build that is not bound to a deploy approval (users and API tokens with direct
// build permission). Approval-bound token builds are validated by validateTokenBuildBinding instead.
// app comes from the rack path the build is forwarded to.
func (h *Handler) validateBuildManifestForAllUsers(r *http.Request, app string, vals url.Values) error {
	// Without the app, its patterns can't be looked up: refuse the build rather than let it through unchecked.
	if app == "" {
		return refuse(http.StatusBadRequest, errors.New("could not determine the app for this build"))
	}
	patterns, err := h.settingsService.GetServiceImagePatterns(app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}
	if len(patterns) == 0 {
		return nil
	}

	gitSHA := strings.TrimSpace(vals.Get("git-sha"))
	if gitSHA == "" {
		return refuse(http.StatusBadRequest,
			errors.New("git-sha is required when image pattern validation is configured"))
	}
	return h.validateBuildManifest(r, app, vals, newImagePolicy(patterns, gitSHA, false))
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
	if objectURL := vals.Get("url"); objectURL == "" || objectURL != approval.ObjectURL {
		return refuse(http.StatusForbidden,
			fmt.Errorf("build must use the archive uploaded for deploy approval %s", approval.PublicID))
	}

	patterns, err := h.settingsService.GetServiceImagePatterns(tracker.app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}
	commit, err := approvalCommit(approval, tracker.app, patterns)
	if err != nil {
		return refuse(http.StatusForbidden, err)
	}
	if gitSHA := strings.TrimSpace(vals.Get("git-sha")); gitSHA != "" && !strings.EqualFold(gitSHA, commit) {
		return refuse(http.StatusForbidden,
			fmt.Errorf("git-sha %s does not match the approved commit %s", gitSHA, commit))
	}

	policy := newImagePolicy(patterns, commit, true)
	if err := h.validateBuildManifest(r, tracker.app, vals, policy); err != nil {
		gtwlog.Warnf("deploy approval %s: build manifest rejected: %v", approval.PublicID, err)
		return err
	}

	gtwlog.Infof("deploy approval %s: build bound to commit %s and object %s", approval.PublicID, commit,
		approval.ObjectURL)
	return nil
}

// manifestPathFrom returns the build's manifest path. validateBuildRequest has already refused one with
// surrounding whitespace, so it's used as sent, like the rack does.
func manifestPathFrom(vals url.Values) string {
	if manifestPath := vals.Get("manifest"); manifestPath != "" {
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

	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil {
		// Not authorized through a deploy approval (e.g. a person's deploy): there's nothing to record.
		gtwlog.Debugf("updateObjectURLApprovalTracking: no deploy approval for objectURL=%s", objectURL)
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
func (h *Handler) updateBuildApprovalTracking(r *http.Request, buildID, releaseID string) error {
	if buildID == "" {
		gtwlog.Warnf("updateBuildApprovalTracking: skipping update (empty buildID)")
		return nil
	}

	// Use the permission-based tracker which is set by RBAC when permission is granted
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil {
		// Not authorized through a deploy approval (e.g. a person's build): there's nothing to record.
		gtwlog.Debugf("updateBuildApprovalTracking: no deploy approval for buildID=%s", buildID)
		return nil
	}

	gtwlog.Infof(
		"updateBuildApprovalTracking: updating approval_id=%d public_id=%s "+
			"buildID=%s releaseID=%s",
		tracker.request.ID,
		tracker.request.PublicID,
		buildID,
		releaseID,
	)

	if err := h.database.MarkDeployApprovalRequestBuildStarted(tracker.request.ID, buildID, releaseID); err != nil {
		return err
	}
	gtwlog.Infof(
		"updateBuildApprovalTracking: successfully updated approval %d buildID=%s releaseID=%s",
		tracker.request.ID,
		buildID,
		releaseID,
	)
	return nil
}
