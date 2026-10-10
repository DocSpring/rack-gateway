package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// ClientIP resolves the client IP once per request and stores it on the request context, where
// netutil.ClientIP reads it for code that only has the *http.Request. It uses gin's Context.ClientIP, so
// X-Forwarded-For is only believed from a proxy in TRUSTED_PROXY_CIDRS (taking the right-most address
// that isn't a trusted proxy); without trusted proxies the client IP is the TCP peer.
func ClientIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(netutil.WithClientIP(c.Request.Context(), c.ClientIP()))
		c.Next()
	}
}
