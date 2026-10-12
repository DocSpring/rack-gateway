package proxy

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// canonicalRackRequest returns r with its path set to the rack path it's authorized and forwarded as, so that every
// check reading the app or an ID from the request path sees the path RBAC and the rack see.
//
// RBAC and forwarding strip one gateway prefix (rbac.NormalizeRackPath). The CLI route has already stripped
// /api/v1/rack-proxy, so /api/v1/rack-proxy/convox/apps/myapp/builds arrives as /convox/apps/myapp/builds, and is
// authorized and forwarded as /apps/myapp/builds; a check reading the app from the unstripped path found none and
// skipped the build's manifest validation. A path that a second normalization would change
// (/convox/convox/apps/...) has no single meaning (RBAC normalizes again; the rack wouldn't), so it's refused.
//
// The path the client sent is kept in X-Original-Path for the audit log, unless a route already set it.
func canonicalRackRequest(r *http.Request) (*http.Request, bool) {
	rackPath := rbac.NormalizeRackPath(r.URL.Path)
	if rbac.NormalizeRackPath(rackPath) != rackPath {
		return r, false
	}
	if rackPath == r.URL.Path {
		return r, true
	}

	canonical := r.Clone(r.Context())
	if canonical.Header.Get("X-Original-Path") == "" {
		canonical.Header.Set("X-Original-Path", r.URL.Path)
	}
	canonical.URL.Path = rackPath
	canonical.URL.RawPath = ""
	return canonical, true
}
