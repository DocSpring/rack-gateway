package proxy

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type headerSet map[string]struct{}

func newHeaderSet(names ...string) headerSet {
	set := make(headerSet, len(names))
	for _, name := range names {
		set[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	return set
}

func (s headerSet) has(name string) bool {
	_, ok := s[http.CanonicalHeaderKey(name)]
	return ok
}

// forwardedClientHeaders lists the client request headers the gateway passes on to the rack API.
// Derived from the Convox SDK (sdk.Client.Headers, stdsdk.Client.Request) and the `header:"..."` option
// tags in convox pkg/structs (LogsOptions, ProcessExecOptions, ProcessRunOptions, ObjectOptions).
//
// Everything else is dropped: client credentials and cookies, actor and tenant overrides (X-Convox-Actor,
// X-Convox-TID), X-Forwarded-*, hop-by-hop headers, Accept-Encoding (so the transport decompresses responses
// before the gateway masks secrets in them), gateway-internal headers, and env headers (env changes must go
// through the release body, where the gateway checks protected and secret keys).
var forwardedClientHeaders = newHeaderSet(
	// Sent by the SDK on every request
	"Accept", "Content-Type", "User-Agent", "Version",
	// Logs
	"Filter", "Follow", "Maxlogrequests", "Prefix", "Previous", "Since", "Tail",
	// Exec (the SDK sends the exec command as a header)
	"Command", "Disable-Stdin", "Entrypoint", "Height", "Tty", "Width",
	// Run
	"Cpu", "Cpu-Limit", "Gpu", "Gpu-Vendor", "Memory", "Memory-Limit", "Release", "Retain",
	"Termination-Grace", "Use-Service-Lifecycle", "Use-Service-Volume",
	// Object uploads
	"Public",
)

// privilegedRunHeaders are process run options that let a one-off process escape the app's release image or
// scheduling constraints: a custom image, host-path volumes, privileged mode, node placement and tolerations,
// system-critical priority, and arbitrary pod labels/annotations (which can route service traffic to the pod
// or relax security profiles). Only callers holding convox:process:run_privileged may send them, and API
// tokens never may.
var privilegedRunHeaders = newHeaderSet(
	"Image", "Volumes", "Privileged", "Node-Labels", "Node-Affinity", "Run-Tolerations",
	"System-Critical", "Run-Annotations", "Run-Labels",
)

// copyForwardedHeaders copies only allowlisted client headers into dst.
func copyForwardedHeaders(dst, src http.Header) {
	for key, values := range src {
		canonical := http.CanonicalHeaderKey(key)
		if !forwardedClientHeaders.has(canonical) && !privilegedRunHeaders.has(canonical) {
			continue
		}
		for _, value := range values {
			dst.Add(canonical, value)
		}
	}
}

// setGatewayHeaders adds the rack credential and the identity the rack should record for the request.
// The actor header replaces anything the client sent, so the rack's own audit trail names the real caller.
func setGatewayHeaders(dst http.Header, rack config.RackConfig, authUser *auth.User) {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))
	dst.Set("Authorization", "Basic "+credentials)
	dst.Set("X-Request-ID", uuid.New().String())
	if authUser == nil {
		return
	}
	dst.Set("X-User-Email", authUser.Email)
	dst.Set("X-Convox-Actor", rackActor(authUser))
}

// rackActor names the caller in the rack's audit trail: the user's email, or token:<name> for API tokens.
func rackActor(authUser *auth.User) string {
	if !authUser.IsAPIToken {
		return authUser.Email
	}
	name := strings.TrimSpace(authUser.TokenName)
	if name == "" && authUser.TokenID != nil {
		name = strconv.FormatInt(*authUser.TokenID, 10)
	}
	return "token:" + name
}

// disallowedRunOptions returns the privileged run options in r that the caller may not use, sorted.
func (h *Handler) disallowedRunOptions(r *http.Request, authUser *auth.User) []string {
	var requested []string
	for key := range r.Header {
		if privilegedRunHeaders.has(key) {
			requested = append(requested, http.CanonicalHeaderKey(key))
		}
	}
	if len(requested) == 0 {
		return nil
	}
	if authUser != nil && !authUser.IsAPIToken && h.callerCan(r, rbac.ResourceProcess, rbac.ActionRunPrivileged) {
		return nil
	}
	sort.Strings(requested)
	return requested
}

// buildTargetURL constructs the rack URL for a proxied request. The path is the decoded path the gateway
// authorized; it is re-escaped so characters like '?' and '#' cannot change the route the rack sees.
func buildTargetURL(rack config.RackConfig, path string, rawQuery string) (string, error) {
	base, err := url.Parse(strings.TrimRight(rack.URL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid rack URL: %w", err)
	}
	target := *base
	target.Path = base.Path + "/" + strings.TrimLeft(path, "/")
	target.RawPath = ""
	target.RawQuery = rawQuery
	target.Fragment = ""
	return target.String(), nil
}

// isSafeRackPath rejects decoded paths the rack could interpret differently from the gateway's route
// matcher: control characters, '?' or '#' smuggled in via percent-encoding, and dot segments.
func isSafeRackPath(path string) bool {
	for _, r := range path {
		if r < 0x20 || r == 0x7f || r == '?' || r == '#' {
			return false
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
