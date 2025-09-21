package routes

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
	Resource string
	Action   string
}

var specs = []RouteSpec{
	// Processes
	{"GET", "/apps/{app}/processes", "process", "list"},
	{"GET", "/apps/{app}/processes/{pid}", "process", "get"},
	{"DELETE", "/apps/{app}/processes/{pid}", "process", "terminate"},
	{"SOCKET", "/apps/{app}/processes/{pid}/exec", "process", "exec"},
	{"GET", "/apps/{app}/processes/{pid}/exec", "process", "exec"},
	{"POST", "/apps/{app}/services/{service}/processes", "process", "start"},

	// Logs (app/system/build)
	{"SOCKET", "/apps/{app}/processes/{pid}/logs", "log", "read"},
	{"SOCKET", "/apps/{app}/builds/{id}/logs", "log", "read"},
	{"SOCKET", "/apps/{app}/logs", "log", "read"},
	{"SOCKET", "/system/logs", "log", "read"},
	// Also allow audit mapping for GET (WS upgrades are GETs)
	{"GET", "/apps/{app}/processes/{pid}/logs", "log", "read"},
	{"GET", "/apps/{app}/builds/{id}/logs", "log", "read"},
	{"GET", "/apps/{app}/logs", "log", "read"},
	{"GET", "/system/logs", "log", "read"},

	// Builds
	{"GET", "/apps/{app}/builds", "build", "list"},
	{"GET", "/apps/{app}/builds/{id}", "build", "get"},
	{"GET", "/apps/{app}/builds/{id}.tgz", "build", "get"},
	{"POST", "/apps/{app}/builds", "build", "create"},
	{"POST", "/apps/{app}/builds/import", "build", "create"},
	{"PUT", "/apps/{app}/builds/{id}", "build", "update"},

	// Releases
	{"GET", "/apps/{app}/releases", "release", "list"},
	{"GET", "/apps/{app}/releases/{id}", "release", "get"},
	{"POST", "/apps/{app}/releases", "release", "create"},
	{"POST", "/apps/{app}/releases/{id}/promote", "release", "promote"},

	// Objects (deploy bundle upload)
	{"POST", "/apps/{app}/objects/tmp/{name}", "object", "create"},

	// Environment (legacy + current)
	{"GET", "/apps/{app}/env", "env", "view"},
	{"POST", "/apps/{app}/env", "env", "set"},
	{"GET", "/apps/{app}/environment", "env", "view"},
	{"POST", "/apps/{app}/environment", "env", "set"},

	// Apps
	{"GET", "/apps", "app", "list"},
	{"GET", "/apps/{name}", "app", "get"},
	{"POST", "/apps", "app", "create"},
	{"PUT", "/apps/{name}", "app", "update"},
	{"POST", "/apps/{app}/restart", "app", "restart"},
	{"DELETE", "/apps/{name}", "app", "delete"},

	// Services
	{"PUT", "/apps/{app}/services/{name}", "app", "update"},
	{"GET", "/apps/{app}/services", "app", "get"},
	{"POST", "/apps/{app}/services/{service}/restart", "process", "start"},

	// Instances
	{"GET", "/instances", "rack", "read"},
	{"GET", "/instances/{id}", "rack", "read"},

	// System
	{"GET", "/system", "rack", "read"},
	{"PUT", "/system", "rack", "update"},
	{"GET", "/system/capacity", "rack", "read"},
	{"GET", "/system/metrics", "rack", "read"},
	{"GET", "/system/processes", "rack", "read"},
	{"GET", "/system/releases", "rack", "read"},
}

// Match returns the resource/action for a given method and path. If not matched, ok=false.
func Match(method, path string) (string, string, bool) {
	for _, s := range specs {
		if s.Method == method && keyMatch3(path, s.Pattern) {
			return s.Resource, s.Action, true
		}
	}
	return "", "", false
}

// AllPermissions returns the list of known convox:<resource>:<action> strings derived from route specs.
func AllPermissions() []string {
	set := make(map[string]struct{})
	for _, s := range specs {
		perm := fmt.Sprintf("convox:%s:%s", s.Resource, s.Action)
		set[perm] = struct{}{}
	}
	// Additional permissions that are not represented in the route specs but exist in RBAC policies.
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
func keyMatch3(path, pattern string) bool {
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
