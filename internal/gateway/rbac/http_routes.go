package rbac

import (
	"strings"

	"github.com/DocSpring/rack-gateway/internal/util/stringset"
)

// RouteAccess selects how an authenticated gateway route is authorized.
type RouteAccess uint8

const (
	// AccessPermissions requires the caller to hold every permission in RouteSpec.Permissions.
	// A route with this access and no permissions is denied.
	AccessPermissions RouteAccess = iota
	// AccessAuthenticated allows any authenticated caller (self-service endpoints).
	// Permissions on these routes only select the MFA level.
	AccessAuthenticated
)

// RouteSpec defines a known Convox API route and the canonical resource/action it maps to.
// Resource names are singular (app, build, release, process, log, object, rack, env).
// Actions are verbs like list, get, create, update, delete, promote, read, exec, start, stop.
type RouteSpec struct {
	Method  string
	Pattern string
	// Permissions contains explicit permission strings for this route. Rack routes set
	// this to the canonical convox:<resource>:<action> permission; HTTP routes supply
	// the gateway/auth permissions that authorize the route and select its MFA level.
	Permissions []string
	Resource    Resource
	Action      Action
	// Access selects how an HTTP route is authorized (see RouteAccess).
	Access RouteAccess
	// AllowAPIToken permits API tokens on an HTTP route. Without it, only human users may call it.
	AllowAPIToken bool
	// SelfParam names a path parameter holding a user's email. When it equals the calling human
	// user's email, the caller may use the route for their own account without the route's
	// permissions. API tokens never qualify. Empty means the route has no self-service rule.
	SelfParam string
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

// newHTTPRoute declares a gateway route for human users that requires every listed permission.
func newHTTPRoute(method, pattern string, permissions ...string) RouteSpec {
	return RouteSpec{
		Method:      method,
		Pattern:     pattern,
		Permissions: permissions,
	}
}

// newTokenRoute declares a gateway route that requires every listed permission and also accepts API tokens.
func newTokenRoute(method, pattern string, permissions ...string) RouteSpec {
	spec := newHTTPRoute(method, pattern, permissions...)
	spec.AllowAPIToken = true
	return spec
}

// newSelfRoute declares a self-service route open to any authenticated human user.
// mfaPermissions only select the MFA level; handlers scope the data to the caller.
func newSelfRoute(method, pattern string, mfaPermissions ...string) RouteSpec {
	spec := newHTTPRoute(method, pattern, mfaPermissions...)
	spec.Access = AccessAuthenticated
	return spec
}

// newOwnUserRoute declares a /users/:email route that every human user may call for their own
// account, and that requires every listed permission for anyone else's account.
func newOwnUserRoute(method, pattern string, permissions ...string) RouteSpec {
	spec := newHTTPRoute(method, pattern, permissions...)
	spec.SelfParam = "email"
	return spec
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

// LookupHTTPRoute returns the route spec declared for an authenticated gateway route.
func LookupHTTPRoute(method, pattern string) (RouteSpec, bool) {
	spec, ok := httpRouteIndex[httpRouteKey(method, pattern)]
	return spec, ok
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
