package routematch

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// RouteSpec defines a known Convox API route and the canonical resource/action it maps to.
// Resource names are singular (app, build, release, process, log, object, rack, env).
// Actions are verbs like list, get, create, update, delete, promote, read, exec, start, stop.
type RouteSpec struct {
	Method   string
	Pattern  string
	Resource rbac.Resource
	Action   rbac.Action
}

// GetMFALevel returns the MFA level required for this route
// Panics if the permission is not defined in MFARequirements
func (r *RouteSpec) GetMFALevel() rbac.MFALevel {
	perm := fmt.Sprintf("convox:%s:%s", r.Resource, r.Action)
	level, ok := rbac.MFARequirements[perm]
	if !ok {
		panic(fmt.Sprintf("CRITICAL: Permission %q not found in MFARequirements - route %s %s", perm, r.Method, r.Pattern))
	}
	return level
}

// RequiresMFAStepUp returns true if this route requires MFA step-up (time window)
func (r *RouteSpec) RequiresMFAStepUp() bool {
	return r.GetMFALevel() >= rbac.MFAStepUp
}

// RequiresMFAAlways returns true if this route requires immediate MFA
func (r *RouteSpec) RequiresMFAAlways() bool {
	return r.GetMFALevel() == rbac.MFAAlways
}

// GetMFALevelForPermission returns the MFA level for a given permission string
// Returns (MFALevel, true) if found, or (MFANone, false) if not found
func GetMFALevelForPermission(permission string) (rbac.MFALevel, bool) {
	level, ok := rbac.MFARequirements[permission]
	return level, ok
}

var specs = []RouteSpec{
	// Processes
	{"GET", "/apps/{app}/processes", rbac.ResourceProcess, rbac.ActionList},
	{"GET", "/apps/{app}/processes/{pid}", rbac.ResourceProcess, rbac.ActionRead},
	{"DELETE", "/apps/{app}/processes/{pid}", rbac.ResourceProcess, rbac.ActionTerminate},
	{"SOCKET", "/apps/{app}/processes/{pid}/exec", rbac.ResourceProcess, rbac.ActionExec},
	{"GET", "/apps/{app}/processes/{pid}/exec", rbac.ResourceProcess, rbac.ActionExec},
	{"POST", "/apps/{app}/services/{service}/processes", rbac.ResourceProcess, rbac.ActionStart},

	// Logs (app/system/build)
	{"SOCKET", "/apps/{app}/processes/{pid}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"SOCKET", "/apps/{app}/builds/{id}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"SOCKET", "/apps/{app}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"SOCKET", "/system/logs", rbac.ResourceLog, rbac.ActionRead},
	// Also allow audit mapping for GET (WS upgrades are GETs)
	{"GET", "/apps/{app}/processes/{pid}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"GET", "/apps/{app}/builds/{id}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"GET", "/apps/{app}/logs", rbac.ResourceLog, rbac.ActionRead},
	{"GET", "/system/logs", rbac.ResourceLog, rbac.ActionRead},

	// Builds
	{"GET", "/apps/{app}/builds", rbac.ResourceBuild, rbac.ActionList},
	{"GET", "/apps/{app}/builds/{id}", rbac.ResourceBuild, rbac.ActionRead},
	{"GET", "/apps/{app}/builds/{id}.tgz", rbac.ResourceBuild, rbac.ActionRead},
	{"POST", "/apps/{app}/builds", rbac.ResourceBuild, rbac.ActionCreate},
	{"POST", "/apps/{app}/builds/import", rbac.ResourceBuild, rbac.ActionCreate},
	{"PUT", "/apps/{app}/builds/{id}", rbac.ResourceBuild, rbac.ActionUpdate},

	// Releases
	{"GET", "/apps/{app}/releases", rbac.ResourceRelease, rbac.ActionList},
	{"GET", "/apps/{app}/releases/{id}", rbac.ResourceRelease, rbac.ActionRead},
	{"POST", "/apps/{app}/releases", rbac.ResourceRelease, rbac.ActionCreate},
	{"POST", "/apps/{app}/releases/{id}/promote", rbac.ResourceRelease, rbac.ActionPromote},

	// Objects (deploy bundle upload)
	{"POST", "/apps/{app}/objects/tmp/{name}", rbac.ResourceObject, rbac.ActionCreate},

	// Apps
	{"GET", "/apps", rbac.ResourceApp, rbac.ActionList},
	{"GET", "/apps/{name}", rbac.ResourceApp, rbac.ActionRead},
	{"POST", "/apps", rbac.ResourceApp, rbac.ActionCreate},
	{"PUT", "/apps/{name}", rbac.ResourceApp, rbac.ActionUpdate},
	{"POST", "/apps/{app}/restart", rbac.ResourceApp, rbac.ActionRestart},
	{"DELETE", "/apps/{name}", rbac.ResourceApp, rbac.ActionDelete},

	// Services
	{"PUT", "/apps/{app}/services/{name}", rbac.ResourceApp, rbac.ActionUpdate},
	{"GET", "/apps/{app}/services", rbac.ResourceApp, rbac.ActionRead},
	{"POST", "/apps/{app}/services/{service}/restart", rbac.ResourceProcess, rbac.ActionStart},

	// Instances
	{"GET", "/instances", rbac.ResourceInstance, rbac.ActionList},
	{"GET", "/instances/{id}", rbac.ResourceInstance, rbac.ActionRead},

	// System
	{"GET", "/system", rbac.ResourceRack, rbac.ActionRead},
	{"PUT", "/system", rbac.ResourceRack, rbac.ActionUpdate},
	{"GET", "/system/capacity", rbac.ResourceRack, rbac.ActionRead},
	{"GET", "/system/metrics", rbac.ResourceRack, rbac.ActionRead},
	{"GET", "/system/processes", rbac.ResourceRack, rbac.ActionRead},
	{"GET", "/system/releases", rbac.ResourceRack, rbac.ActionRead},
}

// Match returns the resource/action for a given method and path. If not matched, ok=false.
func Match(method, path string) (rbac.Resource, rbac.Action, bool) {
	for _, s := range specs {
		if s.Method == method && KeyMatch3(path, s.Pattern) {
			return s.Resource, s.Action, true
		}
	}
	return 0, 0, false
}

// AllPermissions returns the list of known convox:<resource>:<action> strings derived from route specs.
func AllPermissions() []string {
	set := make(map[string]struct{})
	for _, s := range specs {
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

// IsAllowed returns true if a path+method is known.
func IsAllowed(method, path string) bool {
	_, _, ok := Match(method, path)
	return ok
}

// keyMatch3 simplified: supports {var} placeholders and wildcards
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
