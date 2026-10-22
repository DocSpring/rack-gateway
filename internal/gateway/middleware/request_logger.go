package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

// RequestLogger logs HTTP requests to the audit system with user and rack context. A request a handler logs itself
// (the rack proxy does, with its own decision) isn't logged again; every other one is, including rack proxy
// requests refused before the proxy saw them.
func RequestLogger(logger *audit.Logger, defaultRack string, devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Request = c.Request.WithContext(audit.TrackRequestLog(c.Request.Context()))
		c.Next()

		if logger == nil {
			return
		}

		path := extractRequestPath(c)
		if !shouldLogRequest(c, path, devMode) {
			return
		}

		userEmail := extractUserEmail(c)
		rackInfo := extractRackInfo(c, defaultRack)
		rbacDecision := requestDecision(c)

		logger.LogRequest(c.Request, userEmail, rackInfo, rbacDecision, c.Writer.Status(), time.Since(start), nil)
	}
}
