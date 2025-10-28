package middleware

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// ClientIPFromRequest extracts the client IP address from an HTTP request,
// checking X-Forwarded-For, X-Real-IP, and RemoteAddr in that order.
func ClientIPFromRequest(r *http.Request) string {
	return netutil.ClientIPFromRequest(r)
}
