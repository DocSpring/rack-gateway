package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

func shouldSkipLogging(path string, devMode bool) bool {
	if path == "/api/v1/health" || strings.HasPrefix(path, "/.well-known/") || path == "/favicon.ico" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/rack-proxy/") {
		return true
	}
	if devMode && !strings.HasPrefix(path, "/api/v1/") {
		trimmed := strings.TrimPrefix(path, "/")
		if strings.Contains(trimmed, ".") || strings.Contains(trimmed, "@") {
			return true
		}
	}
	return false
}

func extractRequestPath(c *gin.Context) string {
	path := c.Request.Header.Get("X-Original-Path")
	if path == "" {
		path = c.Request.URL.Path
	}
	return path
}

func extractUserEmail(c *gin.Context) string {
	userEmail := strings.TrimSpace(c.Request.Header.Get("X-User-Email"))
	if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
		if strings.TrimSpace(authUser.Email) != "" {
			userEmail = authUser.Email
		}
	}
	return userEmail
}

func extractRackInfo(c *gin.Context, defaultRack string) string {
	r := strings.TrimSpace(defaultRack)
	if r == "" {
		if alias := strings.TrimSpace(c.Request.Header.Get("X-Rack-Alias")); alias != "" {
			r = alias
		} else if name := strings.TrimSpace(c.Request.Header.Get("X-Rack-Name")); name != "" {
			r = name
		}
	}
	return r
}

func extractRBACDecision(c *gin.Context) string {
	rbacDecision := strings.TrimSpace(c.Request.Header.Get("X-RBAC-Decision"))
	if rbacDecision == "" {
		rbacDecision = "allow"
	}
	return rbacDecision
}

func shouldLogRequest(c *gin.Context, path string, devMode bool) bool {
	if audit.RequestAlreadyLogged(c.Request) {
		return false
	}
	return !shouldSkipLogging(path, devMode)
}
