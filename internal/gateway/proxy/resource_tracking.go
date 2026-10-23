package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// recordResourceCreator records email's user as the creator of a resource the request created, unless a creator is
// recorded already, and reports whether it recorded one. The rack has already created the resource, so a failed
// write is tried once more and then logged as an error.
func (h *Handler) recordResourceCreator(resourceType, resourceID, email string) bool {
	if h.database == nil || h.rbacManager == nil {
		return false
	}
	userID, ok := h.creatorID(resourceType, resourceID, email)
	if !ok {
		return false
	}
	var created bool
	err := retryOnce(func() error {
		var err error
		created, err = h.database.CreateUserResource(userID, resourceType, resourceID)
		return err
	})
	if err != nil {
		gtwlog.Errorf("can't record %s as the creator of %s %s: %v", email, resourceType, resourceID, err)
		return false
	}
	return created
}

// setResourceCreator records email's user as the creator of a resource the request just created, replacing the
// record of an earlier resource with the same ID (an app deleted and created again). A failed write is tried once
// more, then logged as an error.
func (h *Handler) setResourceCreator(resourceType, resourceID, email string) {
	userID, ok := h.creatorID(resourceType, resourceID, email)
	if !ok {
		return
	}
	if err := retryOnce(func() error {
		return h.database.SetResourceCreator(userID, resourceType, resourceID)
	}); err != nil {
		gtwlog.Errorf("can't record %s as the creator of %s %s: %v", email, resourceType, resourceID, err)
	}
}

// creatorID returns the ID of email's user, who created a resource, or logs why it can't.
func (h *Handler) creatorID(resourceType, resourceID, email string) (int64, bool) {
	u, err := h.rbacManager.GetUserWithID(email)
	if err != nil || u == nil {
		gtwlog.Errorf("can't load user %s to record them as the creator of %s %s: %v",
			email, resourceType, resourceID, err)
		return 0, false
	}
	return u.ID, true
}

// retryOnce runs write, and runs it again if it fails.
func retryOnce(write func() error) error {
	if err := write(); err == nil {
		return nil
	}
	return write()
}

// captureResourceCreator persists the creator information for app/build/release create responses
// and records the resource ID for audit logging. For a tracked answer (mustTrackAnswer), it returns a
// stepNotRecordedError when what the rack created can't be recorded, or the answer doesn't say what that was.
func (h *Handler) captureResourceCreator(r *http.Request, path string, body []byte, email string, tracked bool) error {
	if h.database == nil || h.rbacManager == nil {
		gtwlog.Warnf(
			"captureResourceCreator: skipping (database=%v rbacManager=%v)",
			h.database != nil,
			h.rbacManager != nil,
		)
		return nil
	}

	obj, ok := h.parseResponseBody(body)
	if !ok {
		gtwlog.Warnf("captureResourceCreator: can't read the response body path=%s", path)
		if tracked {
			return unreadableTrackedAnswer(r, path)
		}
		return nil
	}

	gtwlog.Infof(
		"captureResourceCreator: processing path=%s email=%s",
		path,
		email,
	)
	h.captureAppCreation(r, path, obj, email)
	buildErr := h.captureBuildCreation(r, path, obj, email, tracked)
	objectErr := h.captureObjectUpload(r, path, obj, email, tracked)
	detailsErr := h.captureBuildDetails(r, path, obj)
	h.captureReleaseCreation(r, path, obj, email)
	h.captureProcessResource(r, path, obj, email)
	return errors.Join(buildErr, objectErr, detailsErr)
}

func (_ *Handler) parseResponseBody(body []byte) (map[string]interface{}, bool) {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	obj, ok := payload.(map[string]interface{})
	return obj, ok
}

func (h *Handler) setResourceWithAudit(
	r *http.Request,
	resourceType, resourceID, email string,
	setAudit bool,
) {
	if strings.TrimSpace(resourceID) == "" {
		return
	}
	created := h.recordResourceCreator(resourceType, resourceID, email)
	if setAudit && created {
		r.Header.Set("X-Audit-Resource", resourceID)
	}
}

func (h *Handler) captureAppCreation(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps") {
		if name := extractJSONString(obj["name"]); name != "" {
			r.Header.Set("X-Audit-Resource", name)
			h.setResourceCreator("app", name, email)
		}
	}
}

func (h *Handler) captureBuildCreation(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
	tracked bool,
) error {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(path, "/apps/{app}/builds") {
		return nil
	}

	buildID := extractJSONString(obj["id"])
	releaseID := extractJSONString(obj["release"])

	gtwlog.Infof(
		"captureBuildCreation: extracted buildID=%s releaseID=%s from response",
		buildID,
		releaseID,
	)

	if buildID != "" {
		h.setResourceWithAudit(r, "build", buildID, email, true)
		h.nameCheckedBuild(r, buildID)
	}
	if releaseID != "" {
		if h.recordResourceCreator("release", releaseID, email) {
			r.Header.Add("X-Release-Created", releaseID)
		}
	}

	if buildID == "" {
		if tracked {
			return unreadableAnswer("the rack answered the build request without the build's ID", retryBuild)
		}
		gtwlog.Warnf("captureBuildCreation: skipping updateBuildApprovalTracking (empty buildID)")
		return nil
	}
	// Track build_id in deploy approval immediately (release may not be set yet). The build claim is given up with
	// the error, so the build can be created again.
	return recordInApproval("the rack created build "+buildID, retryBuild, func() error {
		return h.updateBuildApprovalTracking(r, buildID, releaseID)
	})
}

func (h *Handler) captureObjectUpload(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
	tracked bool,
) error {
	if !isObjectUpload(r, path) {
		return nil
	}

	segments := strings.Split(strings.TrimSpace(path), "/")
	if len(segments) > 0 {
		filename := segments[len(segments)-1]
		if filename != "" {
			r.Header.Set("X-Audit-Resource", filename)
		}
	}

	key := h.extractObjectKey(obj, segments)
	if key != "" {
		h.setResourceWithAudit(r, "object", key, email, false)
	}

	// Convox API returns "Url" with capital U (Go struct field name). Only a stored object answers with one, and the
	// rack stores it at the key it was sent to. A build of the object needs that key marked uploaded, and the key
	// can't be written again (object keys are write-once), so an upload that doesn't name it, or can't be marked,
	// has to be made again, to a new key.
	objectURL := extractJSONString(obj["Url"])
	if objectURL != uploadObjectURL(path) {
		if !tracked {
			return nil
		}
		if objectURL == "" {
			return unreadableAnswer("the rack answered the upload without the stored object's URL", retryUpload)
		}
		return unreadableAnswer("the rack answered the upload with another object's URL, "+objectURL, retryUpload)
	}
	done := "the rack stored " + objectURL
	if err := recordStep(done, "record that the upload finished", retryUpload, func() error {
		return h.database.MarkObjectUploaded(extractAppFromPath(path), uploadObjectKey(path))
	}); err != nil {
		return err
	}
	return recordInApproval(done, retryUpload, func() error {
		return h.updateObjectURLApprovalTracking(r, objectURL)
	})
}

func (_ *Handler) extractObjectKey(obj map[string]interface{}, segments []string) string {
	key := extractJSONString(obj["key"])
	if key == "" {
		key = extractJSONString(obj["id"])
	}
	if key == "" && len(segments) > 0 {
		key = segments[len(segments)-1]
	}
	return key
}

// recordBuildRelease records the build's creator as the creator of its release. A build created outside the gateway
// has no creator, and neither does its release.
func (h *Handler) recordBuildRelease(buildID, releaseID string) {
	creatorID, ok, err := h.database.GetResourceCreator("build", buildID)
	if err != nil {
		gtwlog.Warnf("recordBuildRelease: can't read the creator of build %s: %v", buildID, err)
		return
	}
	if !ok {
		return
	}
	if err := retryOnce(func() error {
		_, err := h.database.CreateUserResource(creatorID, "release", releaseID)
		return err
	}); err != nil {
		gtwlog.Errorf("recordBuildRelease: can't record the creator of release %s: %v", releaseID, err)
	}
}

// captureBuildDetails records a build's release once a read of the build shows it. The rack creates the release when
// the build finishes, so whoever reads the build didn't create it: the release is credited to the build's creator,
// and the read writes no release.create audit entry.
func (h *Handler) captureBuildDetails(r *http.Request, path string, obj map[string]interface{}) error {
	if !rbac.KeyMatch3(path, "/apps/{app}/builds/{id}") {
		return nil
	}

	buildID := extractJSONString(obj["id"])
	releaseID := extractJSONString(obj["release"])
	if buildID != "" && releaseID != "" {
		h.recordBuildRelease(buildID, releaseID)

		// Record the approval's release once its build has one. Every later read of the build skips this: the
		// release is never replaced.
		tracker := getDeployApprovalTracker(r.Context())
		if hasApproval(tracker) && tracker.request.ReleaseID == "" && buildID != "" {
			gtwlog.Infof(
				"captureBuildDetails: updating approval with releaseID=%s for buildID=%s",
				releaseID,
				buildID,
			)
			return recordInApproval("build "+buildID+" has release "+releaseID, "read the build again",
				func() error { return h.updateBuildApprovalTracking(r, buildID, releaseID) })
		}
	}
	return nil
}

func (h *Handler) captureReleaseCreation(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(path, "/apps/{app}/releases") {
		return
	}

	if id := extractJSONString(obj["id"]); id != "" {
		r.Header.Set("X-Audit-Resource", id)
		if h.recordResourceCreator("release", id, email) {
			r.Header.Add("X-Release-Created", id)
		}
	}
}

func (h *Handler) captureProcessResource(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(path, "/apps/{app}/services/{service}/processes") {
		return
	}

	if id := extractJSONString(obj["id"]); id != "" {
		r.Header.Set("X-Audit-Resource", id)
		h.setResourceWithAudit(r, "process", id, email, false)
	}
}
