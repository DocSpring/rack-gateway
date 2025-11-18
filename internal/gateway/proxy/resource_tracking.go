package proxy

import (
	"encoding/json"
	"net/http"
	"strings"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// recordResourceCreator stores the user->resource mapping if possible
func (h *Handler) recordResourceCreator(resourceType, resourceID, email string) bool {
	if h.database == nil || h.rbacManager == nil {
		return false
	}
	u, err := h.rbacManager.GetUserWithID(email)
	if err != nil || u == nil {
		return false
	}
	created, err := h.database.CreateUserResource(u.ID, resourceType, resourceID)
	if err != nil {
		return false
	}
	return created
}

// captureResourceCreator persists the creator information for app/build/release create responses
// and records the resource ID for audit logging.
func (h *Handler) captureResourceCreator(r *http.Request, path string, body []byte, email string) {
	if h.database == nil || h.rbacManager == nil {
		gtwlog.Warnf(
			"captureResourceCreator: skipping (database=%v rbacManager=%v)",
			h.database != nil,
			h.rbacManager != nil,
		)
		return
	}
	if len(body) == 0 {
		gtwlog.Warnf("captureResourceCreator: skipping (empty body) path=%s", path)
		return
	}

	obj, ok := h.parseResponseBody(body)
	if !ok {
		gtwlog.Warnf(
			"captureResourceCreator: failed to parse response body path=%s",
			path,
		)
		return
	}

	gtwlog.Infof(
		"captureResourceCreator: processing path=%s email=%s",
		path,
		email,
	)
	h.captureAppCreation(r, path, obj, email)
	h.captureBuildCreation(r, path, obj, email)
	h.captureObjectUpload(r, path, obj, email)
	h.captureBuildDetails(r, path, obj, email)
	h.captureReleaseCreation(r, path, obj, email)
	h.captureProcessResource(r, path, obj, email)
}

func (h *Handler) parseResponseBody(body []byte) (map[string]interface{}, bool) {
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
			h.setResourceWithAudit(r, "app", name, email, true)
		}
	}
}

func (h *Handler) captureBuildCreation(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(path, "/apps/{app}/builds") {
		return
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
	}
	if releaseID != "" {
		if h.recordResourceCreator("release", releaseID, email) {
			r.Header.Add("X-Release-Created", releaseID)
		}
	}

	// Track build_id in deploy approval immediately (release may not be set yet)
	if buildID != "" {
		tracker := getDeployApprovalTracker(r.Context())
		gtwlog.Infof(
			"captureBuildCreation: calling updateBuildApprovalTracking "+
				"buildID=%s releaseID=%s tracker=%v",
			buildID,
			releaseID,
			tracker != nil,
		)
		h.updateBuildApprovalTracking(r, buildID, releaseID)
	} else {
		gtwlog.Warnf(
			"captureBuildCreation: skipping updateBuildApprovalTracking (empty buildID)",
		)
	}
}

func (h *Handler) captureObjectUpload(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(path, "/apps/{app}/objects/tmp/{name}") {
		return
	}

	gtwlog.Infof(
		"[OBJECT_UPLOAD] captureObjectUpload: START path=%s email=%s",
		path,
		email,
	)

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

	objectURL := extractJSONString(obj["url"])
	gtwlog.Infof(
		"[OBJECT_UPLOAD] captureObjectUpload: extracted objectURL=%s from response",
		objectURL,
	)

	if objectURL != "" {
		// Check if tracker exists in context BEFORE calling updateObjectURLApprovalTracking
		val := r.Context().Value(deployApprovalContextKey)
		tracker, ok := val.(*deployApprovalTracker)
		gtwlog.Infof(
			"[OBJECT_UPLOAD] captureObjectUpload: context check - has_value=%v is_tracker=%v tracker_nil=%v",
			val != nil,
			ok,
			tracker == nil,
		)
		if tracker != nil {
			gtwlog.Infof(
				"[OBJECT_UPLOAD] captureObjectUpload: tracker found - approval_id=%d tokenID=%d app=%s",
				tracker.request.ID,
				tracker.tokenID,
				tracker.app,
			)
		}

		if err := h.updateObjectURLApprovalTracking(r, objectURL); err != nil {
			gtwlog.Errorf("proxy: failed to update object URL tracking after validation passed: %v", err)
		}
	} else {
		gtwlog.Warnf("[OBJECT_UPLOAD] captureObjectUpload: no objectURL in response body")
	}
}

func (h *Handler) extractObjectKey(obj map[string]interface{}, segments []string) string {
	key := extractJSONString(obj["key"])
	if key == "" {
		key = extractJSONString(obj["id"])
	}
	if key == "" && len(segments) > 0 {
		key = segments[len(segments)-1]
	}
	return key
}

func (h *Handler) captureBuildDetails(
	r *http.Request,
	path string,
	obj map[string]interface{},
	email string,
) {
	if !rbac.KeyMatch3(path, "/apps/{app}/builds/{id}") {
		return
	}

	buildID := extractJSONString(obj["id"])
	releaseID := extractJSONString(obj["release"])

	if buildID != "" {
		h.recordResourceCreator("build", buildID, email)
	}
	if releaseID != "" {
		if h.recordResourceCreator("release", releaseID, email) {
			r.Header.Add("X-Release-Created", releaseID)
		}

		// Update deploy approval with release_id if it was set after build completion
		tracker := getDeployApprovalTracker(r.Context())
		if tracker != nil && buildID != "" {
			gtwlog.Infof(
				"captureBuildDetails: updating approval with releaseID=%s for buildID=%s",
				releaseID,
				buildID,
			)
			h.updateBuildApprovalTracking(r, buildID, releaseID)
		}
	}
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
