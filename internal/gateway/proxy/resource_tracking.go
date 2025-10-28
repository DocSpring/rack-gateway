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
		return
	}
	if len(body) == 0 {
		return
	}

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	setResource := func(resourceType, resourceID string, setAudit bool) bool {
		if strings.TrimSpace(resourceID) == "" {
			return false
		}
		created := h.recordResourceCreator(resourceType, resourceID, email)
		if setAudit && created {
			r.Header.Set("X-Audit-Resource", resourceID)
		}
		return created
	}

	obj, ok := payload.(map[string]interface{})
	if !ok {
		return
	}

	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps") {
		if name := extractJSONString(obj["name"]); name != "" {
			setResource("app", name, true)
		}
	}

	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/builds") {
		buildID := extractJSONString(obj["id"])
		releaseID := extractJSONString(obj["release"])

		if buildID != "" {
			setResource("build", buildID, true)
		}
		if releaseID != "" {
			if h.recordResourceCreator("release", releaseID, email) {
				r.Header.Add("X-Release-Created", releaseID)
			}
		}

		// Update deploy approval tracking with build_id and release_id
		if buildID != "" && releaseID != "" {
			h.updateBuildApprovalTracking(r, buildID, releaseID)
		}
	}
	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/objects/tmp/{name}") {
		// Extract filename from path for audit logging
		segments := strings.Split(strings.TrimSpace(path), "/")
		if len(segments) > 0 {
			filename := segments[len(segments)-1]
			if filename != "" {
				r.Header.Set("X-Audit-Resource", filename)
			}
		}

		// Track object key for resource creator
		key := extractJSONString(obj["key"])
		if key == "" {
			key = extractJSONString(obj["id"])
		}
		if key == "" && len(segments) > 0 {
			key = segments[len(segments)-1]
		}
		if key != "" {
			setResource("object", key, false)
		}

		// Track object URL for deploy approval workflow
		if objectURL := extractJSONString(obj["url"]); objectURL != "" {
			// This should never fail because we validated upfront
			if err := h.updateObjectURLApprovalTracking(r, objectURL); err != nil {
				// Log error but don't fail - we already validated this should work
				gtwlog.Errorf("proxy: failed to update object URL tracking after validation passed: %v", err)
			}
		}
	}

	if rbac.KeyMatch3(path, "/apps/{app}/builds/{id}") {
		if id := extractJSONString(obj["id"]); id != "" {
			h.recordResourceCreator("build", id, email)
		}
		if rel := extractJSONString(obj["release"]); rel != "" {
			if h.recordResourceCreator("release", rel, email) {
				r.Header.Add("X-Release-Created", rel)
			}
		}
	}

	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/releases") {
		if id := extractJSONString(obj["id"]); id != "" {
			r.Header.Set("X-Audit-Resource", id)
			if h.recordResourceCreator("release", id, email) {
				r.Header.Add("X-Release-Created", id)
			}
		}
	}

	if r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/services/{service}/processes") {
		if id := extractJSONString(obj["id"]); id != "" {
			r.Header.Set("X-Audit-Resource", id)
			setResource("process", id, false)
		}
	}
}
