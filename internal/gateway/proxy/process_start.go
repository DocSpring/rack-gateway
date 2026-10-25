package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// processStartRoute is where `convox run` starts a one-off process.
const processStartRoute = "/apps/{app}/services/{service}/processes"

// headerOnlyRoutes are rack routes that take their options only from headers: the rack reads a process start's
// options with stdapi.UnmarshalOptions, and every ProcessRunOptions field is a `header` tag, while a service restart
// takes no options. The Convox SDK sends neither a body nor a query string to them. The gateway checks a process
// start's headers (the release a deploy approval is bound to, the approved command, the privileged run options), so
// a body or query string, which a later rack might read in place of a header, is refused rather than forwarded
// unchecked.
var headerOnlyRoutes = []string{processStartRoute, "/apps/{app}/services/{service}/restart"}

// checkHeaderOnlyRequest refuses a body or query string on a request to one of headerOnlyRoutes.
func checkHeaderOnlyRequest(r *http.Request, path string, body []byte) error {
	if r.Method != http.MethodPost || !matchesAnyRoute(path, headerOnlyRoutes) {
		return nil
	}
	if r.URL.RawQuery != "" {
		return refuse(http.StatusBadRequest, errors.New("this request takes no query parameters"))
	}
	if len(body) > 0 {
		return refuse(http.StatusBadRequest, errors.New("this request takes no body: its options are headers"))
	}
	return nil
}

// placeholderRunOptions are the process start options that can make a process run something besides its command:
// the privileged run options (a custom image runs its own entrypoint) and the service's lifecycle hooks.
var placeholderRunOptions = func() headerSet {
	set := newHeaderSet("Use-Service-Lifecycle")
	for name := range privilegedRunHeaders {
		set[name] = struct{}{}
	}
	return set
}()

// isAttachedRunPlaceholder reports whether r starts a process that runs nothing but `convox run`'s placeholder
// command (convox.AttachedRunPlaceholder), so it needs no MFA proof of its own: the real command comes as an exec,
// which does.
func isAttachedRunPlaceholder(r *http.Request) bool {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(r.URL.Path, processStartRoute) {
		return false
	}
	commands := r.Header.Values("Command")
	if len(commands) != 1 || strings.TrimSpace(commands[0]) != convox.AttachedRunPlaceholder {
		return false
	}
	for name := range r.Header {
		if placeholderRunOptions.has(name) {
			return false
		}
	}
	return true
}

func matchesAnyRoute(path string, routes []string) bool {
	normalized := "/" + strings.TrimLeft(path, "/")
	for _, route := range routes {
		if rbac.KeyMatch3(normalized, route) {
			return true
		}
	}
	return false
}

// validateProcessCommand refuses a process start under a deploy approval whose command isn't one of the app's
// approved_deploy_commands, or whose approval is no longer approved and unexpired now that the whole request has
// been read (the approval was looked up when it began). The refusal, which the audit log keeps, names the command
// without the credentials it may carry.
func (h *Handler) validateProcessCommand(r *http.Request, original string) error {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(original, processStartRoute) {
		return nil
	}

	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil {
		return nil
	}

	app := extractAppFromPath(original)
	command := strings.TrimSpace(r.Header.Get("Command"))
	if command != convox.AttachedRunPlaceholder && !h.isCommandApproved(app, command) {
		return refuse(http.StatusForbidden, fmt.Errorf("command not approved: %s", audit.RedactCommand(command)))
	}

	authorizes, err := h.database.DeployApprovalAuthorizes(tracker.request.ID)
	if err != nil {
		return err
	}
	if !authorizes {
		return refuse(http.StatusForbidden,
			fmt.Errorf("deploy approval %s is no longer approved", tracker.request.PublicID))
	}
	return nil
}
