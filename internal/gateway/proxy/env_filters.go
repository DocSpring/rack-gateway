package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func (h *Handler) filterReleaseEnvForUser(r *http.Request, body []byte, app string) []byte {
	canEnvView := h.callerCan(r, rbac.ResourceEnv, rbac.ActionRead)

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	var transform func(string) string
	if !canEnvView {
		transform = maskAllEnvVars
	} else {
		keys := h.loadAppEnvKeys(app)
		transform = func(s string) string {
			return maskSecretAndProtectedEnvVars(s, keys)
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

// maskSecretAndProtectedEnvVars masks secret env vars, app-specific protected keys and values with credentials in
// them.
func maskSecretAndProtectedEnvVars(s string, keys appEnvKeys) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		if len(parts) > 1 && keys.masks(parts[0], parts[1]) {
			lines[i] = parts[0] + "=" + maskedSecret
		}
	}
	return strings.Join(lines, "\n")
}

// secretNameList returns the names CONVOX_SECRET_ENV_VARS marks secret.
func (h *Handler) secretNameList() []string {
	names := make([]string, 0, len(h.secretNames))
	for k := range h.secretNames {
		names = append(names, k)
	}
	return names
}

// auditVisibleKeys returns the env vars whose values the app's env change audit entries may show. When the
// app's settings can't be read, none are shown.
func (h *Handler) auditVisibleKeys(app string) envutil.AuditVisibleKeys {
	if h.settingsService == nil || app == "" {
		return envutil.AuditVisibleKeys{}
	}
	keys, err := envutil.LoadAuditVisibleKeys(h.settingsService, app, h.secretNameList())
	if err != nil {
		gtwlog.Errorf("proxy: can't read the audit env settings for app %q, so every value is redacted: %v", app, err)
	}
	return keys
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

// captureProcessCreation records a process started under a deploy approval in it: the approval's later steps on the
// process (exec, terminate) find it by that record. It returns a stepNotRecordedError when it can't, including when
// the rack's answer doesn't name the process.
func (h *Handler) captureProcessCreation(_ *http.Request, body []byte, tracker *deployApprovalTracker) error {
	if h.database == nil || !hasApproval(tracker) {
		return nil
	}

	const retry = "start a new process (this one can't be used under the approval)"
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return unreadableAnswer("the rack answered a process start with a body that isn't a process", retry)
	}
	processID, ok := resp["id"].(string)
	if !ok || processID == "" {
		return unreadableAnswer("the rack answered a process start without the process's ID", retry)
	}

	return recordInApproval("the rack started process "+processID, retry, func() error {
		return h.database.AppendProcessIDToDeployApprovalRequest(tracker.request.ID, processID)
	})
}

// approvedCommand returns command's entry in the app's approved_deploy_commands, and whether it has one. An empty
// list approves no commands under a deploy approval (fail closed), and so does one the gateway can't read.
func (h *Handler) approvedCommand(app, command string) (settings.ApprovedCommand, bool) {
	if h.settingsService == nil {
		return settings.ApprovedCommand{}, false
	}

	approvedCommands, err := h.settingsService.GetApprovedDeployCommands(app)
	if err != nil {
		log.Printf("Failed to get approved commands for app %s: %v", logutil.SanitizeForLog(app), err)
		return settings.ApprovedCommand{}, false
	}

	for _, approved := range approvedCommands {
		if strings.TrimSpace(command) == strings.TrimSpace(approved.Command) {
			return approved, true
		}
	}

	return settings.ApprovedCommand{}, false
}

// isCommandApproved reports whether command is in the app's approved_deploy_commands (see approvedCommand).
func (h *Handler) isCommandApproved(app, command string) bool {
	_, ok := h.approvedCommand(app, command)
	return ok
}

// filterEnvironmentMapResponse masks values in the GET /apps/{app}/environment response.
// The environment endpoint returns a flat JSON map: {"KEY1": "value1", "KEY2": "value2"}
// This is different from the release format which has an "env" field with newline-separated values.
func (h *Handler) filterEnvironmentMapResponse(r *http.Request, body []byte, app string) []byte {
	canEnvView := h.callerCan(r, rbac.ResourceEnv, rbac.ActionRead)

	var envMap map[string]string
	if err := json.Unmarshal(body, &envMap); err != nil {
		return body
	}

	// Without env:read every value is masked; with it, secrets, protected keys and values with credentials are.
	var keys appEnvKeys
	if canEnvView {
		keys = h.loadAppEnvKeys(app)
	}
	for key, value := range envMap {
		if !canEnvView || keys.masks(key, value) {
			envMap[key] = maskedSecret
		}
	}

	result, err := json.Marshal(envMap)
	if err != nil {
		return body
	}
	return result
}
