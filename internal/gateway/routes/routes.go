package routes

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
)

// Config holds dependencies needed for route setup
type Config struct {
	*deps.Gateway
	App interface{} // Reference to app for handlers that need it
}

// Request body limits for the gateway's own API. The rack proxy (/api/v1/rack-proxy) has its own: archive uploads
// stream through, and every other body is capped as it's read (internal/gateway/proxy).
const (
	// maxUnauthenticatedBodyBytes covers login and the CLI login flow, whose bodies are a few small JSON fields.
	maxUnauthenticatedBodyBytes = 64 << 10
	// maxAPIBodyBytes covers the signed-in API. The largest body is the web env editor's changes, at most the app's
	// whole env, which comes to tens of KB; settings bodies are short lists. The cap applies before authentication,
	// so it stays close to that: several concurrent oversized bodies must not exhaust the gateway's memory.
	maxAPIBodyBytes = 1 << 20
)

// Setup configures all routes for the application
func Setup(router *gin.Engine, cfg *Config) {
	setupGlobalMiddleware(router, cfg)

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/.well-known/") {
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	})

	setupCORS(router, cfg)

	h := initializeHandlers(cfg)

	registerStaticRoutes(router, h)

	api := router.Group("/api/v1")
	registerAuthRoutes(api, cfg, h)
	api.GET("/health", h.health.Health)

	authenticated := api.Group("")
	authenticated.Use(middleware.LimitRequestBody(maxAPIBodyBytes))
	authenticated.Use(middleware.Authenticated(cfg.AuthService))
	authenticated.Use(middleware.Authorize(cfg.RBACManager, cfg.AuditLogger))
	authenticated.Use(middleware.RequireMFAEnrollmentWeb(cfg.Database, cfg.SettingsService))
	authenticated.Use(middleware.RequireVerifiedMFASession(cfg.MFAService, cfg.Database, cfg.SettingsService))
	authenticated.Use(middleware.EnforceMFARequirements(cfg.MFAService, cfg.Database, cfg.SettingsService))
	authenticated.Use(middleware.AuditBeforeActing(cfg.AuditLogger, cfg.Database))

	registerMFARoutes(authenticated, cfg, h)
	registerAPIRoutes(authenticated, cfg, h)
	registerAdminRoutes(authenticated, cfg, h)
	registerCLIProxyRoutes(api, cfg, h)
}
