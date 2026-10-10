package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// approvalApps is the set of apps whose deploy approval requests a command acts on: a list of app names, or
// every app (--all-apps). CI deploys several apps for one commit (e.g. docspring and api-proxy), each with its
// own request.
type approvalApps struct {
	names []string // sorted and deduplicated; empty when all is set
	all   bool
}

const appsFlagHelp = "app name, or a comma-separated list of apps " +
	"(default: auto-detected from .convox/app or the current directory)"

const allAppsFlagHelp = "act on the requests of every app for the commit"

// resolveApprovalApps parses --app (one app or a comma-separated list) and --all-apps. With neither, the app
// is resolved like other commands (CONVOX_APP, .convox/app, directory name).
func resolveApprovalApps(appFlag string, allApps bool) (approvalApps, error) {
	appFlag = strings.TrimSpace(appFlag)
	if allApps {
		if appFlag != "" {
			return approvalApps{}, errors.New("use either --app or --all-apps, not both")
		}
		return approvalApps{all: true}, nil
	}
	if !strings.Contains(appFlag, ",") {
		app, err := ResolveApp(appFlag)
		if err != nil {
			return approvalApps{}, err
		}
		return approvalApps{names: []string{app}}, nil
	}

	seen := map[string]bool{}
	var names []string
	for _, part := range strings.Split(appFlag, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return approvalApps{}, fmt.Errorf("invalid --app %q: empty app name", appFlag)
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return approvalApps{names: names}, nil
}

// includes reports whether requests for app are in scope.
func (a approvalApps) includes(app string) bool {
	if a.all {
		return true
	}
	for _, name := range a.names {
		if name == app {
			return true
		}
	}
	return false
}

// queryApp is the app to filter on server-side: the app when there is exactly one, otherwise "" (fetch every
// app and filter with includes).
func (a approvalApps) queryApp() string {
	if !a.all && len(a.names) == 1 {
		return a.names[0]
	}
	return ""
}

func (a approvalApps) String() string {
	if a.all {
		return "all apps"
	}
	return strings.Join(a.names, ", ")
}

// newestRequestPerApp keeps the first (newest) in-scope request of each app, in order. The gateway lists
// requests newest first.
func (a approvalApps) newestRequestPerApp(requests []deployApprovalRequest) []deployApprovalRequest {
	seen := map[string]bool{}
	var result []deployApprovalRequest
	for _, req := range requests {
		if !a.includes(req.App) || seen[req.App] {
			continue
		}
		seen[req.App] = true
		result = append(result, req)
	}
	return result
}
