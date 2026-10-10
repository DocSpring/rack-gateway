package cli

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
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

var fullCommitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// errApprovalTargetNotExplicit explains why an approval without a terminal was refused.
var errApprovalTargetNotExplicit = errors.New("without a terminal, approving needs an explicit --app " +
	"(one app or a comma-separated list) and a full 40-character --commit, and no --branch or --all-apps: " +
	"the PIN dialog shows only this command, so it must say exactly what is approved")

// requireExplicitApprovalTarget refuses an approval run without a terminal (e.g. by an AI agent) unless its
// command line names exactly what it approves: the person typing the PIN sees only the command line, once.
func requireExplicitApprovalTarget(interactive bool, appFlag string, allApps bool, branch, commit string) error {
	if interactive {
		return nil
	}
	if strings.TrimSpace(appFlag) == "" || allApps || strings.TrimSpace(branch) != "" ||
		!fullCommitSHA.MatchString(strings.TrimSpace(commit)) {
		return errApprovalTargetNotExplicit
	}
	return nil
}

// displayText makes CI-supplied text (request messages, app and branch names) safe to print: control characters
// (including terminal escape sequences) and bidirectional overrides are replaced, so a request can't rewrite the
// terminal or hide text from the person or agent reading it.
func displayText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return '?'
		}
		return r
	}, s)
}
