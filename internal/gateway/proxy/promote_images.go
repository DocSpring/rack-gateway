package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// checkPromoteImages refuses a promote whose release would run an image the app's service_image_patterns don't
// allow. A build's images were checked when it was created, but a promote loads the release's manifest again,
// with the release's env substituted into it (Convox common.ReleaseManifest), and takes its init container and
// resource images from that. The env can have changed since the build, so the gateway loads the manifest the same
// way before forwarding the promote. An approval-bound promote is checked even without patterns (it's refused, as
// its build would have been), and its images must be tagged with the approved commit.
func (h *Handler) checkPromoteImages(r *http.Request, rackPath string, tracker *deployApprovalTracker) error {
	if !isPromoteRequest(r, rackPath) {
		return nil
	}
	app, releaseID := extractAppFromPath(rackPath), extractReleaseIDFromPath(rackPath)
	if app == "" || releaseID == "" {
		return refuse(http.StatusBadRequest, errors.New("could not determine the app and release to promote"))
	}
	patterns, err := h.settingsService.GetServiceImagePatterns(app)
	if err != nil {
		return fmt.Errorf("failed to get service image patterns: %w", err)
	}
	var approval *db.DeployApprovalRequest
	if tracker != nil {
		approval = tracker.request
	}
	if len(patterns) == 0 && approval == nil {
		return nil
	}
	var policy imagePolicy
	if approval != nil {
		commit, err := approvalCommit(approval, app, patterns)
		if err != nil {
			return refuse(http.StatusForbidden, err)
		}
		policy = newImagePolicy(patterns, commit, true)
	}

	release, err := h.fetchRelease(r.Context(), app, releaseID)
	if err != nil {
		return refuse(http.StatusBadGateway, fmt.Errorf("failed to check release %s's images: %w", releaseID, err))
	}
	if approval == nil {
		if policy, err = h.buildCommitPolicy(r.Context(), app, release, patterns); err != nil {
			return err
		}
	}
	if err := checkReleaseImages(release, policy); err != nil {
		return refuse(http.StatusForbidden, fmt.Errorf("promote of release %s refused: %w", releaseID, err))
	}
	return nil
}

// buildCommitPolicy is the image policy for promoting a release that no deploy approval covers: {{GIT_COMMIT}} in
// the patterns is the commit the release's build was made from (the git-sha the gateway required when the build was
// created).
func (h *Handler) buildCommitPolicy(
	ctx context.Context,
	app string,
	release *rackRelease,
	patterns map[string]string,
) (imagePolicy, error) {
	commit := ""
	if release.Build != "" {
		build, err := h.fetchBuild(ctx, app, release.Build)
		if err != nil {
			return imagePolicy{}, refuse(http.StatusBadGateway,
				fmt.Errorf("failed to read build %s of release %s: %w", release.Build, release.ID, err))
		}
		commit = strings.TrimSpace(build.GitSha)
	}
	return newImagePolicy(patterns, commit, false), nil
}

// checkReleaseImages loads the release's manifest with its env, as the rack's promote does, and checks the images
// the promote takes from it.
func checkReleaseImages(release *rackRelease, policy imagePolicy) error {
	env, err := parseRackEnv(release.Env)
	if err != nil {
		return err
	}
	manifest, err := loadManifestAsRack([]byte(release.Manifest), env)
	if err != nil {
		return err
	}
	return policy.validateReleaseImages(manifest)
}

// approvalCommit is the full commit SHA a deploy approval is bound to. Its build and promote must use images tagged
// with it, and the commit tag alone doesn't pin the image repository (attacker/image:<sha> would pass), so the app
// must have image patterns that do.
func approvalCommit(approval *db.DeployApprovalRequest, app string, patterns map[string]string) (string, error) {
	commit, ok := db.NormalizeCommitSHA(approval.GitCommitHash)
	if !ok {
		return "", fmt.Errorf("deploy approval %s is not bound to a full git commit SHA", approval.PublicID)
	}
	if len(patterns) == 0 {
		return "", fmt.Errorf(
			"deploy approvals for app %s require service_image_patterns to pin the image repository "+
				`(e.g. {"*": "docker\\.io/org/app:{{GIT_COMMIT}}-amd64"})`,
			app,
		)
	}
	return commit, nil
}
