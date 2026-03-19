package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *Handler) filterReleaseEnvForUser(email string, body []byte, app string) []byte {
	canEnvView, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionRead)

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	var transform func(string) string
	if !canEnvView {
		transform = maskAllEnvVars
	} else {
		// Create a transform that masks secrets AND app-specific protected keys
		transform = func(s string) string {
			return h.maskSecretAndProtectedEnvVars(s, app)
		}
	}

	if updated, ok := transformEnvPayload(payload, transform); ok {
		return updated
	}
	return body
}

// maskAllEnvVars masks all environment variable values.
func maskAllEnvVars(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		if len(parts) == 2 {
			lines[i] = parts[0] + "=" + maskedSecret
		}
	}
	return strings.Join(lines, "\n")
}

// maskSecretAndProtectedEnvVars masks secret env vars and app-specific protected keys.
func (h *Handler) maskSecretAndProtectedEnvVars(s string, app string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		key := parts[0]
		shouldMask := h.isSecretKey(key) || (app != "" && h.isProtectedKeyForApp(key, app))
		if shouldMask && len(parts) > 1 {
			lines[i] = parts[0] + "=" + maskedSecret
		}
	}
	return strings.Join(lines, "\n")
}

func (h *Handler) isSecretKey(key string) bool {
	extra := make([]string, 0, len(h.secretNames)+len(h.protectedEnv))
	for k := range h.secretNames {
		extra = append(extra, k)
	}
	for k := range h.protectedEnv {
		extra = append(extra, k)
	}
	return envutil.IsSecretKey(key, extra)
}

func (h *Handler) isProtectedKeyForApp(key, app string) bool {
	if h.settingsService != nil && app != "" {
		if protectedKeys, err := h.settingsService.GetProtectedEnvVars(app); err == nil {
			upperKey := strings.ToUpper(strings.TrimSpace(key))
			for _, protected := range protectedKeys {
				if strings.ToUpper(strings.TrimSpace(protected)) == upperKey {
					return true
				}
			}
		}
	}
	_, ok := h.protectedEnv[strings.ToUpper(strings.TrimSpace(key))]
	return ok
}

func transformEnvPayload(payload interface{}, transform func(string) string) ([]byte, bool) {
	switch v := payload.(type) {
	case map[string]interface{}:
		return transformEnvMap(v, transform)
	case []interface{}:
		return transformEnvArray(v, transform)
	}
	return nil, false
}

// transformEnvMap transforms environment variables in a map payload.
func transformEnvMap(v map[string]interface{}, transform func(string) string) ([]byte, bool) {
	envv, ok := v["env"].(string)
	if !ok {
		return nil, false
	}

	v["env"] = transform(envv)
	nb, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return nb, true
}

// transformEnvArray transforms environment variables in an array payload.
func transformEnvArray(v []interface{}, transform func(string) string) ([]byte, bool) {
	changed := false
	for _, it := range v {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		envv, ok2 := m["env"].(string)
		if !ok2 {
			continue
		}
		m["env"] = transform(envv)
		changed = true
	}

	if !changed {
		return nil, false
	}

	nb, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return nb, true
}

func (h *Handler) captureProcessCreation(_ *http.Request, body []byte, tracker *deployApprovalTracker) {
	if h.database == nil || tracker == nil {
		return
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("Failed to parse process creation response: %v", err)
		return
	}

	processID, ok := resp["id"].(string)
	if !ok || processID == "" {
		log.Printf("Process ID not found in response")
		return
	}

	if err := h.database.AppendProcessIDToDeployApprovalRequest(tracker.request.ID, processID); err != nil {
		log.Printf("Failed to track process ID in deploy approval: %v", err)
	}
}

func (h *Handler) isCommandApproved(app, command string) bool {
	if h.settingsService == nil {
		return false
	}

	approvedCommands, err := h.settingsService.GetApprovedDeployCommands(app)
	if err != nil {
		log.Printf("Failed to get approved commands for app %s: %v", logutil.SanitizeForLog(app), err)
		return false
	}

	if len(approvedCommands) == 0 {
		return true
	}

	for _, approved := range approvedCommands {
		if strings.TrimSpace(command) == strings.TrimSpace(approved) {
			return true
		}
	}

	return false
}

// filterEnvironmentMapResponse masks secret keys in GET /apps/{app}/environment response.
// The environment endpoint returns a flat JSON map: {"KEY1": "value1", "KEY2": "value2"}
// This is different from the release format which has an "env" field with newline-separated values.
func (h *Handler) filterEnvironmentMapResponse(email string, body []byte, app string) []byte {
	canEnvView, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionRead)

	var envMap map[string]string
	if err := json.Unmarshal(body, &envMap); err != nil {
		return body
	}

	// Build set of keys that need masking
	for key := range envMap {
		shouldMask := false
		if !canEnvView {
			// No env:read permission - mask all values
			shouldMask = true
		} else if h.isSecretKey(key) || h.isProtectedKeyForApp(key, app) {
			// Has env:read - mask secrets and protected keys
			shouldMask = true
		}
		if shouldMask {
			envMap[key] = maskedSecret
		}
	}

	result, err := json.Marshal(envMap)
	if err != nil {
		return body
	}
	return result
}
