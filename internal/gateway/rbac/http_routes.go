package rbac

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RouteSpec defines a known Convox API route and the canonical resource/action it maps to.
// Resource names are singular (app, build, release, process, log, object, rack, env).
// Actions are verbs like list, get, create, update, delete, promote, read, exec, start, stop.
type RouteSpec struct {
	Method   string
	Pattern  string
	Resource Resource
	Action   Action
}

// GetMFALevel returns the MFA level required for this route
// Panics if the permission is not defined in MFARequirements
func (r *RouteSpec) GetMFALevel() MFALevel {
	perm := fmt.Sprintf("convox:%s:%s", r.Resource, r.Action)
	level, ok := MFARequirements[perm]
	if !ok {
		panic(fmt.Sprintf("CRITICAL: Permission %q not found in MFARequirements - route %s %s", perm, r.Method, r.Pattern))
	}
	return level
}

// RequiresMFAStepUp returns true if this route requires MFA step-up (time window)
func (r *RouteSpec) RequiresMFAStepUp() bool {
	return r.GetMFALevel() >= MFAStepUp
}

// RequiresMFAAlways returns true if this route requires immediate MFA
func (r *RouteSpec) RequiresMFAAlways() bool {
	return r.GetMFALevel() == MFAAlways
}

// Route specs for proxied Convox rack requests (rack-proxy endpoints and audit helpers).
var rackRouteSpecs = []RouteSpec{
	// Processes
	{"GET", "/apps/{app}/processes", ResourceProcess, ActionList},
	{"GET", "/apps/{app}/processes/{pid}", ResourceProcess, ActionRead},
	{"DELETE", "/apps/{app}/processes/{pid}", ResourceProcess, ActionTerminate},
	{"SOCKET", "/apps/{app}/processes/{pid}/exec", ResourceProcess, ActionExec},
	{"GET", "/apps/{app}/processes/{pid}/exec", ResourceProcess, ActionExec},
	{"POST", "/apps/{app}/services/{service}/processes", ResourceProcess, ActionStart},

	// Logs (app/system/build)
	{"SOCKET", "/apps/{app}/processes/{pid}/logs", ResourceLog, ActionRead},
	{"SOCKET", "/apps/{app}/builds/{id}/logs", ResourceLog, ActionRead},
	{"SOCKET", "/apps/{app}/logs", ResourceLog, ActionRead},
	{"SOCKET", "/system/logs", ResourceLog, ActionRead},
	{"GET", "/apps/{app}/processes/{pid}/logs", ResourceLog, ActionRead},
	{"GET", "/apps/{app}/builds/{id}/logs", ResourceLog, ActionRead},
	{"GET", "/apps/{app}/logs", ResourceLog, ActionRead},
	{"GET", "/system/logs", ResourceLog, ActionRead},

	// Builds
	{"GET", "/apps/{app}/builds", ResourceBuild, ActionList},
	{"GET", "/apps/{app}/builds/{id}", ResourceBuild, ActionRead},
	{"GET", "/apps/{app}/builds/{id}.tgz", ResourceBuild, ActionRead},
	{"POST", "/apps/{app}/builds", ResourceBuild, ActionCreate},
	{"POST", "/apps/{app}/builds/import", ResourceBuild, ActionCreate},
	{"PUT", "/apps/{app}/builds/{id}", ResourceBuild, ActionUpdate},

	// Releases
	{"GET", "/apps/{app}/releases", ResourceRelease, ActionList},
	{"GET", "/apps/{app}/releases/{id}", ResourceRelease, ActionRead},
	{"POST", "/apps/{app}/releases", ResourceRelease, ActionCreate},
	{"POST", "/apps/{app}/releases/{id}/promote", ResourceRelease, ActionPromote},

	// Objects (deploy bundle upload)
	{"POST", "/apps/{app}/objects/tmp/{name}", ResourceObject, ActionCreate},

	// Apps
	{"GET", "/apps", ResourceApp, ActionList},
	{"GET", "/apps/{name}", ResourceApp, ActionRead},
	{"POST", "/apps", ResourceApp, ActionCreate},
	{"PUT", "/apps/{name}", ResourceApp, ActionUpdate},
	{"POST", "/apps/{app}/restart", ResourceApp, ActionRestart},
	{"DELETE", "/apps/{name}", ResourceApp, ActionDelete},

	// Services
	{"PUT", "/apps/{app}/services/{name}", ResourceApp, ActionUpdate},
	{"GET", "/apps/{app}/services", ResourceApp, ActionRead},
	{"POST", "/apps/{app}/services/{service}/restart", ResourceProcess, ActionStart},

	// Instances
	{"GET", "/instances", ResourceInstance, ActionList},
	{"GET", "/instances/{id}", ResourceInstance, ActionRead},

	// System
	{"GET", "/system", ResourceRack, ActionRead},
	{"PUT", "/system", ResourceRack, ActionUpdate},
	{"GET", "/system/capacity", ResourceRack, ActionRead},
	{"GET", "/system/metrics", ResourceRack, ActionRead},
	{"GET", "/system/processes", ResourceRack, ActionRead},
	{"GET", "/system/releases", ResourceRack, ActionRead},
}

// HTTPRouteSpec defines MFA permissions for first-class gateway endpoints (non-proxied routes).
// Permissions holds either a single static permission or an array generated at
// runtime for dynamic settings endpoints, where we fan out to multiple keys.
// 'Dynamic' signals the middleware to invoke ExtractSettingsPermissions to
// produce the final permission slice before enforcement.
type HTTPRouteSpec struct {
	Method      string
	Pattern     string
	Permissions []string
	Dynamic     bool
}

var httpRouteSpecs = []HTTPRouteSpec{
	// MFA management
	{"GET", "/api/v1/auth/mfa/status", []string{}, false},
	{"POST", "/api/v1/auth/mfa/enroll/totp/start", []string{Auth(ResourceMFAMethod, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/enroll/totp/confirm", []string{Auth(ResourceMFAMethod, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/enroll/yubiotp/start", []string{Auth(ResourceMFAMethod, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/enroll/webauthn/start", []string{Auth(ResourceMFAMethod, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/enroll/webauthn/confirm", []string{Auth(ResourceMFAMethod, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/verify", []string{Auth(ResourceMFAVerification, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/webauthn/assertion/start", []string{Auth(ResourceMFAVerification, ActionCreate)}, false},
	{"POST", "/api/v1/auth/mfa/webauthn/assertion/verify", []string{Auth(ResourceMFAVerification, ActionCreate)}, false},
	{"PUT", "/api/v1/auth/mfa/preferred-method", []string{Auth(ResourceMFAPreferences, ActionUpdate)}, false},
	{"PUT", "/api/v1/auth/mfa/methods/:methodID", []string{Auth(ResourceMFAMethod, ActionUpdate)}, false},
	{"POST", "/api/v1/auth/mfa/backup-codes/regenerate", []string{Auth(ResourceMFABackupCodes, ActionGenerate)}, false},
	{"POST", "/api/v1/auth/mfa/trusted-devices/trust", []string{Auth(ResourceTrustedDevice, ActionCreate)}, false},
	{"DELETE", "/api/v1/auth/mfa/trusted-devices/:deviceID", []string{Auth(ResourceTrustedDevice, ActionDelete)}, false},
	{"DELETE", "/api/v1/auth/mfa/methods/:methodID", []string{Auth(ResourceMFAMethod, ActionDelete)}, false},

	// Authenticated info
	{"GET", "/api/v1/info", []string{}, false},
	{"GET", "/api/v1/created-by", []string{}, false},
	{"GET", "/api/v1/rack", []string{Convox(ResourceRack, ActionRead)}, false},
	{"GET", "/api/v1/env", []string{Convox(ResourceEnv, ActionRead)}, false},
	{"PUT", "/api/v1/env", []string{Convox(ResourceEnv, ActionSet)}, false},
	{"GET", "/api/v1/deploy-approval-requests/:id", []string{Gateway(ResourceDeployApprovalRequest, ActionRead)}, false},
	{"POST", "/api/v1/deploy-approval-requests", []string{Gateway(ResourceDeployApprovalRequest, ActionCreate)}, false},

	// Web-safe Convox proxies
	{"GET", "/api/v1/convox/apps", []string{Convox(ResourceApp, ActionList)}, false},
	{"GET", "/api/v1/convox/apps/*path", []string{Convox(ResourceApp, ActionRead)}, false},
	{"GET", "/api/v1/convox/instances", []string{Convox(ResourceInstance, ActionList)}, false},
	{"GET", "/api/v1/convox/system/processes", []string{Convox(ResourceRack, ActionRead)}, false},

	// Admin configuration & diagnostics
	{"GET", "/api/v1/admin/config", []string{}, false},
	{"PUT", "/api/v1/admin/config", []string{}, false},
	{"GET", "/api/v1/admin/settings", []string{}, false},
	{"PUT", "/api/v1/admin/settings", []string{}, true},
	{"DELETE", "/api/v1/admin/settings", []string{}, true},
	{"POST", "/api/v1/admin/settings/rack_tls_cert/refresh", []string{Security(ResourceSecret, ActionUpdate)}, false},
	{"POST", "/api/v1/admin/diagnostics/sentry", []string{}, false},

	// Admin users & roles
	{"GET", "/api/v1/admin/roles", []string{}, false},
	{"GET", "/api/v1/admin/users", []string{}, false},
	{"GET", "/api/v1/admin/users/:email", []string{}, false},
	{"POST", "/api/v1/admin/users", []string{Gateway(ResourceUser, ActionCreate)}, false},
	{"DELETE", "/api/v1/admin/users/:email", []string{Gateway(ResourceUser, ActionDelete)}, false},
	{"PUT", "/api/v1/admin/users/:email", []string{Gateway(ResourceUser, ActionUpdate)}, false},
	{"PUT", "/api/v1/admin/users/:email/roles", []string{Gateway(ResourceUser, ActionUpdate)}, false},
	{"GET", "/api/v1/admin/users/:email/sessions", []string{}, false},
	{"POST", "/api/v1/admin/users/:email/sessions/:sessionID/revoke", []string{Gateway(ResourceUser, ActionUpdate)}, false},
	{"POST", "/api/v1/admin/users/:email/sessions/revoke_all", []string{Gateway(ResourceUser, ActionUpdate)}, false},
	{"POST", "/api/v1/admin/users/:email/lock", []string{Gateway(ResourceUser, ActionUpdate)}, false},
	{"POST", "/api/v1/admin/users/:email/unlock", []string{Gateway(ResourceUser, ActionUpdate)}, false},

	// Admin audit logs
	{"GET", "/api/v1/admin/audit", []string{Gateway(ResourceDeployApprovalRequest, ActionRead)}, false},
	{"GET", "/api/v1/admin/audit/export", []string{Gateway(ResourceDeployApprovalRequest, ActionRead)}, false},

	// Admin deploy approvals
	{"GET", "/api/v1/admin/deploy-approval-requests", []string{Gateway(ResourceDeployApprovalRequest, ActionRead)}, false},
	{"GET", "/api/v1/admin/deploy-approval-requests/:id/audit-logs", []string{Gateway(ResourceDeployApprovalRequest, ActionRead)}, false},
	{"POST", "/api/v1/admin/deploy-approval-requests/:id/approve", []string{Gateway(ResourceDeployApprovalRequest, ActionApprove)}, false},
	{"POST", "/api/v1/admin/deploy-approval-requests/:id/reject", []string{Gateway(ResourceDeployApprovalRequest, ActionApprove)}, false},

	// Admin API tokens
	{"GET", "/api/v1/admin/tokens", []string{Gateway(ResourceAPIToken, ActionRead)}, false},
	{"GET", "/api/v1/admin/tokens/permissions", []string{}, false},
	{"GET", "/api/v1/admin/tokens/:tokenID", []string{Gateway(ResourceAPIToken, ActionRead)}, false},
	{"POST", "/api/v1/admin/tokens", []string{Gateway(ResourceAPIToken, ActionCreate)}, false},
	{"PUT", "/api/v1/admin/tokens/:tokenID", []string{Gateway(ResourceAPIToken, ActionUpdate)}, false},
	{"DELETE", "/api/v1/admin/tokens/:tokenID", []string{Gateway(ResourceAPIToken, ActionDelete)}, false},

	// Slack integration
	{"GET", "/api/v1/admin/integrations/slack", []string{Gateway(ResourceIntegration, ActionRead)}, false},
	{"POST", "/api/v1/admin/integrations/slack/oauth/authorize", []string{Gateway(ResourceIntegration, ActionCreate)}, false},
	{"GET", "/api/v1/admin/integrations/slack/oauth/callback", []string{Gateway(ResourceIntegration, ActionCreate)}, false},
	{"PUT", "/api/v1/admin/integrations/slack/channels", []string{Gateway(ResourceIntegration, ActionUpdate)}, false},
	{"DELETE", "/api/v1/admin/integrations/slack", []string{Gateway(ResourceIntegration, ActionDelete)}, false},
	{"GET", "/api/v1/admin/integrations/slack/channels/list", []string{Gateway(ResourceIntegration, ActionRead)}, false},
	{"POST", "/api/v1/admin/integrations/slack/test", []string{Gateway(ResourceIntegration, ActionUpdate)}, false},

	// App-specific settings
	{"GET", "/api/v1/apps/:app/settings", []string{}, false},
	{"PUT", "/api/v1/apps/:app/settings", []string{}, true},
	{"DELETE", "/api/v1/apps/:app/settings", []string{}, true},
}

var httpRouteIndex map[string]HTTPRouteSpec

func init() {
	httpRouteIndex = make(map[string]HTTPRouteSpec, len(httpRouteSpecs))
	for _, spec := range httpRouteSpecs {
		key := httpRouteKey(spec.Method, spec.Pattern)
		httpRouteIndex[key] = spec
	}
}

func httpRouteKey(method, pattern string) string {
	return strings.ToUpper(method) + " " + pattern
}

// HTTPMFAPermissions returns the declared permissions for an authenticated gateway route.
func HTTPMFAPermissions(method, pattern string) ([]string, bool) {
	spec, ok := httpRouteIndex[httpRouteKey(method, pattern)]
	if !ok {
		return nil, false
	}
	return spec.Permissions, true
}

// HTTPRouteIsDynamic reports whether the route requires dynamic permission derivation (settings endpoints).
func HTTPRouteIsDynamic(method, pattern string) bool {
	spec, ok := httpRouteIndex[httpRouteKey(method, pattern)]
	return ok && spec.Dynamic
}

// NormalizeRackPath removes API prefixes and returns the canonical Convox path used for routing.
func NormalizeRackPath(path string) string {
	if path == "" {
		return "/"
	}

	normalized := path
	for _, prefix := range []string{"/api/v1/rack-proxy", "/api/v1/convox", "/rack-proxy", "/convox"} {
		if strings.HasPrefix(normalized, prefix) {
			normalized = strings.TrimPrefix(normalized, prefix)
			break
		}
	}

	if normalized == "" {
		normalized = "/"
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized
}

// MatchRackRoute returns the resource/action for a proxied Convox rack request.
func MatchRackRoute(method, path string) (Resource, Action, bool) {
	normalized := NormalizeRackPath(path)
	for _, s := range rackRouteSpecs {
		if s.Method == method && KeyMatch3(normalized, s.Pattern) {
			return s.Resource, s.Action, true
		}
	}
	return 0, 0, false
}

// RackRouteSpecs returns a copy of the known rack route specs (used in tests/helpers).
func RackRouteSpecs() []RouteSpec {
	out := make([]RouteSpec, len(rackRouteSpecs))
	copy(out, rackRouteSpecs)
	return out
}

// RackRouteExample returns a concrete example path matching the RouteSpec pattern.
func RackRouteExample(spec RouteSpec) string {
	var b strings.Builder
	pattern := spec.Pattern
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '{':
			j := i + 1
			for j < len(pattern) && pattern[j] != '}' {
				j++
			}
			name := pattern[i+1 : j]
			b.WriteString(placeholderValue(name))
			i = j
		case '*':
			b.WriteString("extra")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

var placeholderValues = map[string]string{
	"app":      "my-app",
	"build":    "B123",
	"id":       "ID123",
	"name":     "resource",
	"pid":      "process-123",
	"service":  "web",
	"registry": "docker.io",
	"release":  "REL123",
	"resource": "db",
	"instance": "i-1234567890",
}

func placeholderValue(name string) string {
	if v, ok := placeholderValues[name]; ok {
		return v
	}
	return name
}

// RackAllPermissions returns the list of known convox:<resource>:<action> strings derived from rack route specs.
func RackAllPermissions() []string {
	set := make(map[string]struct{})
	for _, s := range rackRouteSpecs {
		perm := fmt.Sprintf("convox:%s:%s", s.Resource, s.Action)
		set[perm] = struct{}{}
	}
	perms := make([]string, 0, len(set))
	for perm := range set {
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	return perms
}

// KeyMatch3 simplified: supports {var} placeholders and wildcards, shared by proxies and audit logic.
func KeyMatch3(path, pattern string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '{' {
			for i < len(pattern) && pattern[i] != '}' {
				i++
			}
			b.WriteString("[^/]+")
			continue
		}
		if c == '*' {
			b.WriteString(".*")
			continue
		}
		if strings.ContainsRune(".+?^$()[]{}|\\", rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteString("$")
	re := b.String()
	ok, _ := regexp.MatchString(re, path)
	return ok
}
