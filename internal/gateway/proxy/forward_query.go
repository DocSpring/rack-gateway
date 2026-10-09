package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// forwardedQueryParams lists the query parameters the Convox SDK sends: the `query:"..."` option tags in
// convox pkg/structs (builds, releases, processes, metrics, budgets, diagnose, system, files) and the
// parameters the hand-written SDK methods add (file, files, server).
//
// The rack reads many options from the query string as well as from headers and the form body (stdapi
// Context.Value checks the form, which includes the query, before headers). Forwarding any other parameter
// would let a client override values the gateway checked in a header or the body: an exec command against
// the approved command list, release env against the secret and protected-key rules, or a build manifest
// against the image patterns. Requests carrying any other parameter are refused.
var forwardedQueryParams = map[string]struct{}{
	"age": {}, "all": {}, "checks": {}, "end": {}, "events": {}, "file": {}, "files": {}, "limit": {},
	"lines": {}, "metrics": {}, "period": {}, "previous": {}, "release": {}, "server": {}, "service": {},
	"services": {}, "start": {}, "tar-extra": {},
}

// disallowedQueryParams returns the query parameters in rawQuery that the gateway does not forward, sorted.
// A query string that does not parse is refused as a whole.
func disallowedQueryParams(rawQuery string) ([]string, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, err
	}
	var disallowed []string
	for key := range values {
		if _, ok := forwardedQueryParams[key]; !ok {
			disallowed = append(disallowed, key)
		}
	}
	sort.Strings(disallowed)
	return disallowed, nil
}

// refuseUnforwardableOptions answers the request with an error and returns its status when it carries query
// parameters the gateway does not forward or privileged run options the caller may not use. It returns 0
// when the request may be forwarded.
func (h *Handler) refuseUnforwardableOptions(w http.ResponseWriter, r *http.Request, authUser *auth.User) int {
	disallowedQuery, err := disallowedQueryParams(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "invalid query string", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	if len(disallowedQuery) > 0 {
		msg := fmt.Sprintf("unsupported query parameters: %s", strings.Join(disallowedQuery, ", "))
		http.Error(w, msg, http.StatusBadRequest)
		return http.StatusBadRequest
	}
	if disallowed := h.disallowedRunOptions(r, authUser); len(disallowed) > 0 {
		msg := fmt.Sprintf("only admins can use these process options: %s", strings.Join(disallowed, ", "))
		http.Error(w, msg, http.StatusForbidden)
		return http.StatusForbidden
	}
	return 0
}
