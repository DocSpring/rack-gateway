package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

func RequestLogger(logger *audit.Logger, defaultRack string, devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if logger == nil {
			return
		}

		path := c.Request.Header.Get("X-Original-Path")
		if path == "" {
			path = c.Request.URL.Path
		}
		if path == "/api/v1/health" || strings.HasPrefix(path, "/.well-known/") || path == "/favicon.ico" {
			return
		}
		if devMode {
			if strings.HasPrefix(path, "/api/v1/") {
				// always log API routes
			} else {
				trimmed := strings.TrimPrefix(path, "/")
				if strings.Contains(trimmed, ".") || strings.Contains(trimmed, "@") {
					return
				}
			}
		}
		if audit.RequestAlreadyLogged(c.Request) {
			return
		}
		if strings.HasPrefix(path, "/api/v1/rack-proxy/") {
			return
		}

		userEmail := strings.TrimSpace(c.Request.Header.Get("X-User-Email"))
		if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
			if strings.TrimSpace(authUser.Email) != "" {
				userEmail = authUser.Email
			}
		}

		r := strings.TrimSpace(defaultRack)
		if r == "" {
			if alias := strings.TrimSpace(c.Request.Header.Get("X-Rack-Alias")); alias != "" {
				r = alias
			} else if name := strings.TrimSpace(c.Request.Header.Get("X-Rack-Name")); name != "" {
				r = name
			}
		}

		rbacDecision := strings.TrimSpace(c.Request.Header.Get("X-RBAC-Decision"))
		if rbacDecision == "" {
			rbacDecision = "allow"
		}

		logger.LogRequest(c.Request, userEmail, r, rbacDecision, c.Writer.Status(), time.Since(start), nil)
	}
}
