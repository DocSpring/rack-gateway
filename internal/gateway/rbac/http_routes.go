package rbac

import (
	"regexp"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/util/stringset"
)

// RouteSpec defines a known Convox API route and the canonical resource/action it maps to.
// Resource names are singular (app, build, release, process, log, object, rack, env).
// Actions are verbs like list, get, create, update, delete, promote, read, exec, start, stop.
type RouteSpec struct {
	Method  string
	Pattern string
	// Permissions contains explicit permission strings for this route. Rack routes set
	// this to the canonical convox:<resource>:<action> permission; HTTP routes supply
	// gateway/auth specific permissions or leave the slice empty for MFANone.
	Permissions []string
	Resource    Resource
	Action      Action
}

// GetMFALevel returns the MFA level required for this route
// Panics if the permission is not defined in MFARequirements
func (r *RouteSpec) GetMFALevel() MFALevel {
	perms := r.PermissionStrings()
	level := MFANone
	for _, perm := range perms {
		pl := GetMFALevel([]string{perm})
		if pl > level {
			level = pl
		}
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

// PermissionStrings returns the explicit permission strings associated with the route.
// May be nil when the route does not require MFA (e.g. read-only endpoints).
func (r *RouteSpec) PermissionStrings() []string {
	if len(r.Permissions) > 0 {
		out := make([]string, len(r.Permissions))
		copy(out, r.Permissions)
		return out
	}
	return nil
}

func newRackRoute(method, pattern string, resource Resource, action Action) RouteSpec {
	return RouteSpec{
		Method:  method,
		Pattern: pattern,
		Permissions: []string{
			Convox(resource, action),
		},
		Resource: resource,
		Action:   action,
	}
}

func newHTTPRoute(method, pattern string, permissions ...string) RouteSpec {
	return RouteSpec{
		Method:      method,
		Pattern:     pattern,
		Permissions: permissions,
	}
}

// Route specs for proxied Convox rack requests (rack-proxy endpoints and audit helpers).
var rackRouteSpecs = []RouteSpec{
	// Processes
	newRackRoute("GET", "/apps/{app}/processes", ResourceProcess, ActionList),
	newRackRoute("GET", "/apps/{app}/processes/{pid}", ResourceProcess, ActionRead),
	newRackRoute("DELETE", "/apps/{app}/processes/{pid}", ResourceProcess, ActionTerminate),
	newRackRoute("SOCKET", "/apps/{app}/processes/{pid}/exec", ResourceProcess, ActionExec),
	newRackRoute("GET", "/apps/{app}/processes/{pid}/exec", ResourceProcess, ActionExec),
	newRackRoute("POST", "/apps/{app}/services/{service}/processes", ResourceProcess, ActionStart),
	// Logs (app/system/build)
	newRackRoute("SOCKET", "/apps/{app}/processes/{pid}/logs", ResourceLog, ActionRead),
	newRackRoute("SOCKET", "/apps/{app}/builds/{id}/logs", ResourceLog, ActionRead),
	newRackRoute("SOCKET", "/apps/{app}/logs", ResourceLog, ActionRead),
	newRackRoute("SOCKET", "/system/logs", ResourceLog, ActionRead),
	newRackRoute("GET", "/apps/{app}/processes/{pid}/logs", ResourceLog, ActionRead),
	newRackRoute("GET", "/apps/{app}/builds/{id}/logs", ResourceLog, ActionRead),
	newRackRoute("GET", "/apps/{app}/logs", ResourceLog, ActionRead),
	newRackRoute("GET", "/system/logs", ResourceLog, ActionRead),
	// Builds
	newRackRoute("GET", "/apps/{app}/builds", ResourceBuild, ActionList),
	newRackRoute("GET", "/apps/{app}/builds/{id}", ResourceBuild, ActionRead),
	newRackRoute("GET", "/apps/{app}/builds/{id}.tgz", ResourceBuild, ActionRead),
	newRackRoute("POST", "/apps/{app}/builds", ResourceBuild, ActionCreate),
	newRackRoute("POST", "/apps/{app}/builds/import", ResourceBuild, ActionCreate),
	newRackRoute("PUT", "/apps/{app}/builds/{id}", ResourceBuild, ActionUpdate),
	// Releases
	newRackRoute("GET", "/apps/{app}/releases", ResourceRelease, ActionList),
	newRackRoute("GET", "/apps/{app}/releases/{id}", ResourceRelease, ActionRead),
	newRackRoute("POST", "/apps/{app}/releases", ResourceRelease, ActionCreate),
	newRackRoute("POST", "/apps/{app}/releases/{id}/promote", ResourceRelease, ActionPromote),

	// Objects (deploy bundle upload)
	newRackRoute("POST", "/apps/{app}/objects/tmp/{name}", ResourceObject, ActionCreate),

	// Apps
	newRackRoute("GET", "/apps", ResourceApp, ActionList),
	newRackRoute("GET", "/apps/{name}", ResourceApp, ActionRead),
	newRackRoute("POST", "/apps", ResourceApp, ActionCreate),
	newRackRoute("PUT", "/apps/{name}", ResourceApp, ActionUpdate),
	newRackRoute("POST", "/apps/{app}/restart", ResourceApp, ActionRestart),
	newRackRoute("DELETE", "/apps/{name}", ResourceApp, ActionDelete),

	// Services
	newRackRoute("PUT", "/apps/{app}/services/{name}", ResourceApp, ActionUpdate),
	newRackRoute("GET", "/apps/{app}/services", ResourceApp, ActionRead),
	newRackRoute("POST", "/apps/{app}/services/{service}/restart", ResourceProcess, ActionStart),

	// Instances
	newRackRoute("GET", "/instances", ResourceInstance, ActionList),
	newRackRoute("GET", "/instances/{id}", ResourceInstance, ActionRead),

	// System
	newRackRoute("GET", "/system", ResourceRack, ActionRead),
	newRackRoute("PUT", "/system", ResourceRack, ActionUpdate),
	newRackRoute("GET", "/system/capacity", ResourceRack, ActionRead),
	newRackRoute("GET", "/system/metrics", ResourceRack, ActionRead),
	newRackRoute("GET", "/system/processes", ResourceRack, ActionRead),
	newRackRoute("GET", "/system/releases", ResourceRack, ActionRead),
}

func routeSlug(segment string) string {
	return strings.ReplaceAll(segment, "_", "-")
}

func appSettingPath(key settings.AppSettingKey) string {
	return "/api/v1/apps/:app/settings/" + routeSlug(key.String())
}

func appSettingsGroupPath(group settings.AppSettingGroup) string {
	return "/api/v1/apps/:app/settings/" + routeSlug(string(group))
}

func globalSettingsGroupPath(group settings.GlobalSettingGroup) string {
	return "/api/v1/settings/" + routeSlug(string(group))
}

func settingsActionPath(segment string) string {
	return "/api/v1/settings/" + routeSlug(segment)
}

var httpRouteSpecs = []RouteSpec{
	// MFA management
	newHTTPRoute("GET", "/api/v1/auth/mfa/status"),
	newHTTPRoute("POST", "/api/v1/auth/mfa/enroll/totp/start", Auth(ResourceMFAMethod, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/enroll/totp/confirm", Auth(ResourceMFAMethod, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/enroll/yubiotp/start", Auth(ResourceMFAMethod, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/enroll/webauthn/start", Auth(ResourceMFAMethod, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/enroll/webauthn/confirm", Auth(ResourceMFAMethod, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/verify", Auth(ResourceMFAVerification, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/webauthn/assertion/start", Auth(ResourceMFAVerification, ActionCreate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/webauthn/assertion/verify", Auth(ResourceMFAVerification, ActionCreate)),
	newHTTPRoute("PUT", "/api/v1/auth/mfa/preferred-method", Auth(ResourceMFAPreferences, ActionUpdate)),
	newHTTPRoute("PUT", "/api/v1/auth/mfa/methods/:methodID", Auth(ResourceMFAMethod, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/backup-codes/regenerate", Auth(ResourceMFABackupCodes, ActionGenerate)),
	newHTTPRoute("POST", "/api/v1/auth/mfa/trusted-devices/trust", Auth(ResourceTrustedDevice, ActionCreate)),
	newHTTPRoute("DELETE", "/api/v1/auth/mfa/trusted-devices/:deviceID", Auth(ResourceTrustedDevice, ActionDelete)),
	newHTTPRoute("DELETE", "/api/v1/auth/mfa/methods/:methodID", Auth(ResourceMFAMethod, ActionDelete)),

	// Authenticated info
	newHTTPRoute("GET", "/api/v1/info"),
	newHTTPRoute("GET", "/api/v1/created-by"),
	newHTTPRoute("GET", "/api/v1/rack", Convox(ResourceRack, ActionRead)),
	newHTTPRoute("GET", "/api/v1/deploy-approval-requests", Gateway(ResourceDeployApprovalRequest, ActionRead)),
	newHTTPRoute("GET", "/api/v1/deploy-approval-requests/:id", Gateway(ResourceDeployApprovalRequest, ActionRead)),
	newHTTPRoute(
		"GET",
		"/api/v1/deploy-approval-requests/:id/audit-logs",
		Gateway(ResourceDeployApprovalRequest, ActionRead),
	),
	newHTTPRoute("POST", "/api/v1/deploy-approval-requests", Gateway(ResourceDeployApprovalRequest, ActionCreate)),
	newHTTPRoute(
		"POST",
		"/api/v1/deploy-approval-requests/:id/approve",
		Gateway(ResourceDeployApprovalRequest, ActionApprove),
	),
	newHTTPRoute(
		"POST",
		"/api/v1/deploy-approval-requests/:id/reject",
		Gateway(ResourceDeployApprovalRequest, ActionApprove),
	),
	newHTTPRoute("GET", "/api/v1/apps/:app/env", Convox(ResourceEnv, ActionRead)),
	newHTTPRoute("PUT", "/api/v1/apps/:app/env", Convox(ResourceEnv, ActionSet)),

	// Web-safe Convox proxies
	newHTTPRoute("GET", "/api/v1/convox/apps", Convox(ResourceApp, ActionList)),
	newHTTPRoute("GET", "/api/v1/convox/apps/*path", Convox(ResourceApp, ActionRead)),
	newHTTPRoute("GET", "/api/v1/convox/instances", Convox(ResourceInstance, ActionList)),
	newHTTPRoute("GET", "/api/v1/convox/system/processes", Convox(ResourceRack, ActionRead)),

	// Configuration & diagnostics
	newHTTPRoute("GET", "/api/v1/settings"),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupMFAConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupMFAConfiguration),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupMFAConfiguration),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupMFAConfiguration),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupAllowDestructive),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupAllowDestructive),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupAllowDestructive),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupAllowDestructive),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupVCSAndCIDefaults),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupVCSAndCIDefaults),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupVCSAndCIDefaults),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupVCSAndCIDefaults),
	),
	newHTTPRoute(
		"PUT",
		globalSettingsGroupPath(settings.GlobalSettingGroupDeployApprovals),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupDeployApprovals),
	),
	newHTTPRoute(
		"DELETE",
		globalSettingsGroupPath(settings.GlobalSettingGroupDeployApprovals),
		GatewayGlobalSettingGroup(settings.GlobalSettingGroupDeployApprovals),
	),
	newHTTPRoute("POST", settingsActionPath("rack_tls_cert/refresh"), Security(ResourceSecret, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/diagnostics/sentry", Gateway(ResourceIntegration, ActionUpdate)),

	// Users & roles
	newHTTPRoute("GET", "/api/v1/roles"),
	newHTTPRoute("GET", "/api/v1/users"),
	newHTTPRoute("GET", "/api/v1/users/:email"),
	newHTTPRoute("POST", "/api/v1/users", Gateway(ResourceUser, ActionCreate)),
	newHTTPRoute("DELETE", "/api/v1/users/:email", Gateway(ResourceUser, ActionDelete)),
	newHTTPRoute("PUT", "/api/v1/users/:email", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("PUT", "/api/v1/users/:email/name", Gateway(ResourceUser, ActionUpdateName)),
	newHTTPRoute("GET", "/api/v1/users/:email/sessions"),
	newHTTPRoute("POST", "/api/v1/users/:email/sessions/:sessionID/revoke", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/sessions/revoke_all", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/lock", Gateway(ResourceUser, ActionUpdate)),
	newHTTPRoute("POST", "/api/v1/users/:email/unlock", Gateway(ResourceUser, ActionUpdate)),

	// Audit logs
	newHTTPRoute("GET", "/api/v1/audit-logs", Gateway(ResourceDeployApprovalRequest, ActionRead)),
	newHTTPRoute("GET", "/api/v1/audit-logs/export", Gateway(ResourceDeployApprovalRequest, ActionRead)),

	// API tokens
	newHTTPRoute("GET", "/api/v1/api-tokens", Gateway(ResourceAPIToken, ActionRead)),
	newHTTPRoute("GET", "/api/v1/api-tokens/permissions"),
	newHTTPRoute("GET", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionRead)),
	newHTTPRoute("POST", "/api/v1/api-tokens", Gateway(ResourceAPIToken, ActionCreate)),
	newHTTPRoute("PUT", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionUpdate)),
	newHTTPRoute("DELETE", "/api/v1/api-tokens/:tokenID", Gateway(ResourceAPIToken, ActionDelete)),

	// Background jobs
	newHTTPRoute("GET", "/api/v1/jobs", Gateway(ResourceJob, ActionList)),
	newHTTPRoute("GET", "/api/v1/jobs/:id", Gateway(ResourceJob, ActionRead)),
	newHTTPRoute("DELETE", "/api/v1/jobs/:id", Gateway(ResourceJob, ActionDelete)),
	newHTTPRoute("POST", "/api/v1/jobs/:id/retry", Gateway(ResourceJob, ActionUpdate)),

	// Integrations
	newHTTPRoute("GET", "/api/v1/integrations/slack", Gateway(ResourceIntegration, ActionRead)),
	newHTTPRoute("POST", "/api/v1/integrations/slack/oauth/authorize", Gateway(ResourceIntegration, ActionCreate)),
	newHTTPRoute("GET", "/api/v1/integrations/slack/oauth/callback", Gateway(ResourceIntegration, ActionCreate)),
	newHTTPRoute("PUT", "/api/v1/integrations/slack/channels", Gateway(ResourceIntegration, ActionUpdate)),
	newHTTPRoute("DELETE", "/api/v1/integrations/slack", Gateway(ResourceIntegration, ActionDelete)),
	newHTTPRoute("GET", "/api/v1/integrations/slack/channels/list", Gateway(ResourceIntegration, ActionRead)),
	newHTTPRoute("POST", "/api/v1/integrations/slack/test", Gateway(ResourceIntegration, ActionUpdate)),

	// App-specific settings
	newHTTPRoute("GET", "/api/v1/apps/:app/settings"),
	newHTTPRoute(
		"PUT",
		appSettingsGroupPath(settings.AppSettingGroupVCSCIDeploy),
		GatewayAppSettingGroup(settings.AppSettingGroupVCSCIDeploy),
	),
	newHTTPRoute(
		"DELETE",
		appSettingsGroupPath(settings.AppSettingGroupVCSCIDeploy),
		GatewayAppSettingGroup(settings.AppSettingGroupVCSCIDeploy),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingProtectedEnvVars),
		GatewayAppSetting(settings.AppSettingProtectedEnvVars),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingProtectedEnvVars),
		GatewayAppSetting(settings.AppSettingProtectedEnvVars),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingSecretEnvVars),
		GatewayAppSetting(settings.AppSettingSecretEnvVars),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingSecretEnvVars),
		GatewayAppSetting(settings.AppSettingSecretEnvVars),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingApprovedDeployCommands),
		GatewayAppSetting(settings.AppSettingApprovedDeployCommands),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingApprovedDeployCommands),
		GatewayAppSetting(settings.AppSettingApprovedDeployCommands),
	),
	newHTTPRoute(
		"PUT",
		appSettingPath(settings.AppSettingServiceImagePatterns),
		GatewayAppSetting(settings.AppSettingServiceImagePatterns),
	),
	newHTTPRoute(
		"DELETE",
		appSettingPath(settings.AppSettingServiceImagePatterns),
		GatewayAppSetting(settings.AppSettingServiceImagePatterns),
	),
}

var httpRouteIndex map[string]RouteSpec

func init() {
	httpRouteIndex = make(map[string]RouteSpec, len(httpRouteSpecs))
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

// HTTPRouteSpecs returns a copy of the compiled HTTP route specifications.
func HTTPRouteSpecs() []RouteSpec {
	out := make([]RouteSpec, len(httpRouteSpecs))
	copy(out, httpRouteSpecs)
	return out
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
		for _, perm := range s.PermissionStrings() {
			set[perm] = struct{}{}
		}
	}
	return stringset.SortedKeys(set)
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
