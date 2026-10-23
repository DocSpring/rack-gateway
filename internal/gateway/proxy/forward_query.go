package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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

// maxListLimit caps the `limit` query parameter, which the rack reads on the release and build list endpoints. The
// rack lists 10 when no limit is given (convox provider/k8s ReleaseList and BuildList), and the Convox CLI only
// sends one for --limit. Each release carries the app's whole env and manifest, and the gateway holds a release list
// in memory to mask the secrets in it, so an unbounded limit could make one response larger than the gateway's
// memory. A larger limit is refused rather than lowered, so the caller isn't shown a silently shortened list.
const maxListLimit = 100

// disallowedQueryParams returns the query parameters in values that the gateway does not forward, sorted.
func disallowedQueryParams(values url.Values) []string {
	var disallowed []string
	for key := range values {
		if _, ok := forwardedQueryParams[key]; !ok {
			disallowed = append(disallowed, key)
		}
	}
	sort.Strings(disallowed)
	return disallowed
}

// validListLimit reports whether every limit in values is a whole number from 1 to maxListLimit. The rack reads the
// first one; all are checked so none slips through.
func validListLimit(values url.Values) bool {
	for _, raw := range values["limit"] {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxListLimit {
			return false
		}
	}
	return true
}

// checkForwardableOptions refuses a request that carries query parameters the gateway does not forward, a list
// limit over maxListLimit, or privileged run options the caller may not use. A query string that does not parse is
// refused as a whole.
func (h *Handler) checkForwardableOptions(r *http.Request, authUser *auth.User) error {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return refuse(http.StatusBadRequest, errors.New("invalid query string"))
	}
	if disallowedQuery := disallowedQueryParams(values); len(disallowedQuery) > 0 {
		return refuse(http.StatusBadRequest,
			fmt.Errorf("unsupported query parameters: %s", strings.Join(disallowedQuery, ", ")))
	}
	if !validListLimit(values) {
		return refuse(http.StatusBadRequest, fmt.Errorf("limit must be a whole number from 1 to %d", maxListLimit))
	}
	if disallowed := h.disallowedRunOptions(r, authUser); len(disallowed) > 0 {
		return refuse(http.StatusForbidden,
			fmt.Errorf("only admins can use these process options: %s", strings.Join(disallowed, ", ")))
	}
	return nil
}
