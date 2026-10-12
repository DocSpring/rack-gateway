package middleware

import "github.com/gin-gonic/gin"

// gatewayInternalHeaders are request headers the gateway sets itself to pass identity and audit details
// between middleware, handlers and the audit logger. Clients must never be able to supply them: they
// would spoof the user, token, resource, RBAC decision, path or request ID recorded in the audit log.
// The request ID pairs a request's audit entries (its attempt and its results), so the gateway generates
// a unique one for every request.
var gatewayInternalHeaders = []string{
	"X-Audit-Resource",
	"X-Request-ID",
	"X-Release-Created",
	"X-Original-Path",
	"X-User-Email",
	"X-User-Name",
	"X-API-Token-ID",
	"X-API-Token-Name",
	"X-Auth-Source",
	"X-RBAC-Decision",
	"X-Rack-Alias",
	"X-Rack-Name",
}

// StripInternalHeaders removes client-supplied copies of gateway-internal headers.
// It must run before any middleware that reads them (only ClientIP, which reads none, runs earlier).
func StripInternalHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range gatewayInternalHeaders {
			c.Request.Header.Del(name)
		}
		c.Next()
	}
}
