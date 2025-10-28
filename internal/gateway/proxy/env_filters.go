package proxy

import (
	"encoding/json"
	"log"
	"strings"

	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *Handler) filterReleaseEnvForUser(email string, body []byte, _ bool) []byte {
	canEnvView, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionRead)
	if !canEnvView {
		var any interface{}
		if err := json.Unmarshal(body, &any); err != nil {
			return body
		}
		maskAll := func(s string) string {
			lines := strings.Split(s, "\n")
			for i, ln := range lines {
				if ln == "" {
					continue
				}
				parts := strings.SplitN(ln, "=", 2)
				if len(parts) == 2 {
					parts[1] = maskedSecret
					lines[i] = parts[0] + "=" + parts[1]
				}
			}
			return strings.Join(lines, "\n")
		}
		if updated, ok := transformEnvPayload(any, maskAll); ok {
			return updated
		}
		return body
	}

	var any interface{}
	if err := json.Unmarshal(body, &any); err != nil {
		return body
	}
	mask := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if ln == "" {
				continue
			}
			parts := strings.SplitN(ln, "=", 2)
			key := parts[0]
			if h.isSecretKey(key) && len(parts) > 1 {
				parts[1] = maskedSecret
				lines[i] = parts[0] + "=" + parts[1]
			}
		}
		return strings.Join(lines, "\n")
	}
	if updated, ok := transformEnvPayload(any, mask); ok {
		return updated
	}
	return body
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

func transformEnvPayload(any interface{}, transform func(string) string) ([]byte, bool) {
	switch v := any.(type) {
	case map[string]interface{}:
		if envv, ok := v["env"].(string); ok {
			v["env"] = transform(envv)
			nb, err := json.Marshal(v)
			if err == nil {
				return nb, true
			}
		}
	case []interface{}:
		changed := false
		for _, it := range v {
			if m, ok := it.(map[string]interface{}); ok {
				if envv, ok2 := m["env"].(string); ok2 {
					m["env"] = transform(envv)
					changed = true
				}
			}
		}
		if changed {
			nb, err := json.Marshal(v)
			if err == nil {
				return nb, true
			}
		}
	}
	return nil, false
}

func (h *Handler) captureProcessCreation(r *http.Request, body []byte, tracker *deployApprovalTracker) {
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
		log.Printf("Failed to get approved commands for app %s: %v", app, err)
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
