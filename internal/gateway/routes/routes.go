package routes

import (
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/handlers"
	"github.com/DocSpring/convox-gateway/internal/gateway/middleware"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

// Config holds dependencies needed for route setup
type Config struct {
	App          interface{} // Reference to app for handlers that need it
	Config       *config.Config
	Database     *db.Database
	RBACManager  rbac.RBACManager
	JWTManager   *auth.JWTManager
	OAuthHandler *auth.OAuthHandler
	AuthService  *auth.AuthService
	TokenService *token.Service
	EmailSender  email.Sender
	ProxyHandler *proxy.Handler
	RackCertMgr  *rackcert.Manager
}

// Setup configures all routes for the application
func Setup(router *gin.Engine, cfg *Config) {
	// Global middleware
	router.Use(requestid.New())
	router.Use(middleware.SecurityHeaders(cfg.Config))
	router.Use(middleware.OriginValidator(cfg.Config))
	router.Use(gin.Recovery())
	router.Use(middleware.FilteredLogger()) // Suppress health check logs

	// CORS configuration - allow requests from the configured domain
	// In production this is set via DOMAIN env var
	// In dev mode, we allow localhost
	allowedOrigins := []string{}
	if cfg.Config.Domain != "" {
		if cfg.Config.Domain == "localhost" {
			allowedOrigins = []string{"http://localhost:*", "http://127.0.0.1:*"}
		} else {
			allowedOrigins = []string{"https://" + cfg.Config.Domain}
		}
	}
	if cfg.Config.DevMode {
		allowedOrigins = append(allowedOrigins, "http://localhost:*")
	}

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = allowedOrigins
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = append(corsConfig.AllowHeaders, "X-CSRF-Token", "Authorization")
	router.Use(cors.New(corsConfig))

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(cfg.OAuthHandler, cfg.Database, cfg.Config)
	apiHandler := handlers.NewAPIHandler(cfg.RBACManager, cfg.Database, cfg.Config, cfg.RackCertMgr)
	adminHandler := handlers.NewAdminHandler(cfg.RBACManager, cfg.Database, cfg.TokenService, cfg.EmailSender, cfg.Config, cfg.RackCertMgr)
	proxyHandler := handlers.NewProxyHandler(cfg.ProxyHandler)
	staticHandler := handlers.NewStaticHandler(cfg.Config)
	healthHandler := handlers.NewHealthHandler()

	// Root redirect
	router.GET("/", handlers.RootRedirect)

	// Static files
	router.GET("/favicon.ico", handlers.Favicon)
	router.GET("/robots.txt", handlers.Robots)

	// Web UI static files
	router.GET("/.gateway/web", handlers.WebRedirect)
	router.GET("/.gateway/web/*filepath", staticHandler.ServeStatic)

	// API routes
	api := router.Group("/.gateway/api")
	{
		// Rate limited auth endpoints
		authGroup := api.Group("")
		authGroup.Use(middleware.RateLimit(cfg.Config))
		{
			// CLI auth flow
			authGroup.POST("/auth/cli/start", authHandler.CLILoginStart)
			authGroup.GET("/auth/cli/callback", authHandler.CLILoginCallback)
			authGroup.POST("/auth/cli/complete", authHandler.CLILoginComplete)

			// Web auth flow
			authGroup.GET("/auth/web/login", authHandler.WebLoginStart)
			authGroup.HEAD("/auth/web/login", authHandler.WebLoginStart)
			authGroup.GET("/auth/web/callback", authHandler.WebLoginCallback)
			authGroup.GET("/auth/web/logout", authHandler.WebLogout)
			authGroup.GET("/auth/web/csrf", authHandler.GetCSRFToken)
		}

		// Health check (no auth)
		api.GET("/health", healthHandler.Health)

		// Authenticated endpoints
		authenticated := api.Group("")
		authenticated.Use(middleware.JWTAuth(cfg.JWTManager, cfg.RBACManager))
		{
			// User API
			authenticated.GET("/me", apiHandler.GetMe)
			authenticated.GET("/created-by", apiHandler.GetCreatedBy)
			authenticated.GET("/rack", apiHandler.GetRackInfo)
			authenticated.GET("/env", apiHandler.GetEnvValues)

			// Convox proxy endpoints (safe GET only for web UI)
			convox := authenticated.Group("/convox")
			{
				convox.GET("/apps", proxyHandler.ProxyStripPrefix)
				convox.GET("/apps/*path", proxyHandler.ProxyStripPrefix)
				convox.GET("/instances", proxyHandler.ProxyStripPrefix)
				convox.GET("/system/processes", proxyHandler.ProxyStripPrefix)
			}

			// Admin endpoints (with CSRF protection)
			admin := authenticated.Group("/admin")
			admin.Use(middleware.CSRF())
			{
				// Config and settings
				admin.GET("/config", adminHandler.GetConfig)
				admin.PUT("/config", adminHandler.UpdateConfig)
				admin.GET("/settings", adminHandler.GetSettings)
				admin.PUT("/settings/protected_env_vars", adminHandler.UpdateProtectedEnvVars)
				admin.PUT("/settings/allow_destructive_actions", adminHandler.UpdateAllowDestructiveActions)
				admin.POST("/settings/rack_tls_cert/refresh", adminHandler.RefreshRackTLSCert)

				// Users and roles
				admin.GET("/roles", adminHandler.ListRoles)
				admin.GET("/users", adminHandler.ListUsers)
				admin.POST("/users", adminHandler.CreateUser)
				admin.DELETE("/users/:email", adminHandler.DeleteUser)
				admin.PUT("/users/:email", adminHandler.UpdateUserProfile)
				admin.PUT("/users/:email/roles", adminHandler.UpdateUserRoles)

				// Audit logs
				admin.GET("/audit", adminHandler.ListAuditLogs)
				admin.GET("/audit/export", adminHandler.ExportAuditLogs)

				// API tokens (rate limit creation)
				tokenGroup := admin.Group("")
				tokenGroup.POST("/tokens", middleware.RateLimit(cfg.Config), adminHandler.CreateAPIToken)
				tokenGroup.GET("/tokens", adminHandler.ListAPITokens)
				tokenGroup.GET("/tokens/permissions", adminHandler.GetTokenPermissionMetadata)
				tokenGroup.GET("/tokens/:tokenID", adminHandler.GetAPIToken)
				tokenGroup.PUT("/tokens/:tokenID", adminHandler.UpdateAPIToken)
				tokenGroup.DELETE("/tokens/:tokenID", adminHandler.DeleteAPIToken)
			}
		}
	}

	// Catch-all: Proxy to Convox (CLI only, no cookie auth)
	router.NoRoute(middleware.CLIOnly(cfg.AuthService), proxyHandler.ProxyToRack)
}
