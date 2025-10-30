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
	authenticated.Use(middleware.Authenticated(cfg.AuthService, cfg.RBACManager))
	authenticated.Use(middleware.RequireMFAEnrollmentWeb(cfg.Database, cfg.MFASettings))
	authenticated.Use(middleware.EnforceMFARequirements(cfg.MFAService, cfg.Database, cfg.MFASettings))

	registerMFARoutes(authenticated, cfg, h)
	registerAPIRoutes(authenticated, cfg, h)
	registerAdminRoutes(authenticated, cfg, h)
	registerCLIProxyRoutes(api, cfg, h)
}
