package routes

import (
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/openapi"
	"github.com/DocSpring/rack-gateway/internal/gateway/proxy"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

// Config holds dependencies needed for route setup
type Config struct {
	App              interface{} // Reference to app for handlers that need it
	Config           *config.Config
	Database         *db.Database
	RBACManager      rbac.RBACManager
	JWTManager       *auth.JWTManager
	SessionManager   *auth.SessionManager
	OAuthHandler     *auth.OAuthHandler
	AuthService      *auth.AuthService
	TokenService     *token.Service
	MFAService       *mfa.Service
	MFASettings      *db.MFASettings
	EmailSender      email.Sender
	ProxyHandler     *proxy.Handler
	RackCertMgr      *rackcert.Manager
	SentryEnabled    bool
	AuditLogger      *audit.Logger
	DefaultRack      string
	SecurityNotifier *security.Notifier
}

// Setup configures all routes for the application
func Setup(router *gin.Engine, cfg *Config) {
	// Global middleware
	if cfg.SentryEnabled {
		options := sentrygin.Options{
			Repanic:         true,
			WaitForDelivery: false,
			Timeout:         2 * time.Second,
		}
		router.Use(sentrygin.New(options))
	}

	requestIDMiddleware := requestid.New(requestid.WithHandler(func(c *gin.Context, rid string) {
		if !cfg.SentryEnabled {
			return
		}
		if hub := sentrygin.GetHubFromContext(c); hub != nil {
			hub.Scope().SetTag("request_id", rid)
		}
	}))
	router.Use(requestIDMiddleware)
	router.Use(middleware.SecurityHeaders(cfg.Config))
	router.Use(middleware.HostValidator(cfg.Config))
	router.Use(middleware.OriginValidator(cfg.Config))
	router.Use(gin.Recovery())
	router.Use(middleware.RequestLogger(cfg.AuditLogger, cfg.DefaultRack))

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
	authHandler := handlers.NewAuthHandler(cfg.OAuthHandler, cfg.Database, cfg.Config, cfg.SessionManager, cfg.MFAService, cfg.MFASettings, cfg.SecurityNotifier)
	apiHandler := handlers.NewAPIHandler(cfg.RBACManager, cfg.Database, cfg.Config, cfg.RackCertMgr, cfg.MFASettings)
	adminHandler := handlers.NewAdminHandler(cfg.RBACManager, cfg.Database, cfg.TokenService, cfg.EmailSender, cfg.Config, cfg.RackCertMgr, cfg.SessionManager, cfg.MFASettings)
	proxyHandler := handlers.NewProxyHandler(cfg.ProxyHandler)
	staticHandler := handlers.NewStaticHandler(cfg.Config, cfg.SessionManager)
	healthHandler := handlers.NewHealthHandler()

	// Root redirect
	router.GET("/", handlers.RootRedirect)

	// Static files
	router.GET("/favicon.ico", handlers.Favicon)
	router.GET("/robots.txt", handlers.Robots)

	// Web UI static files
	router.GET("/.gateway/web", handlers.WebRedirect)
	router.GET("/.gateway/web/*filepath", staticHandler.ServeStatic)

	// API documentation
	openapi.Register(router)

	// API routes
	api := router.Group("/.gateway/api")
	{
		// Rate limited auth endpoints
		authGroup := api.Group("")
		authGroup.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
		{
			// CLI auth flow
			authGroup.POST("/auth/cli/start", authHandler.CLILoginStart)
			authGroup.GET("/auth/cli/callback", authHandler.CLILoginCallback)
			authGroup.POST("/auth/cli/complete", authHandler.CLILoginComplete)
			authGroup.GET("/auth/cli/mfa", authHandler.CLILoginMFAForm)
			authGroup.POST("/auth/cli/mfa", authHandler.CLILoginMFASubmit)

			// Web auth flow
			authGroup.GET("/auth/web/login", authHandler.WebLoginStart)
			authGroup.HEAD("/auth/web/login", authHandler.WebLoginStart)
			authGroup.GET("/auth/web/callback", authHandler.WebLoginCallback)
			authGroup.GET("/auth/web/logout", authHandler.WebLogout)
		}

		// Health check (no auth)
		api.GET("/health", healthHandler.Health)

		// Authenticated endpoints
		authenticated := api.Group("")
		authenticated.Use(middleware.Authenticated(cfg.AuthService, cfg.RBACManager))
		authenticated.Use(middleware.RequireMFAEnrollmentWeb(cfg.Database, cfg.MFASettings))
		{
			mfaGroup := authenticated.Group("/auth/mfa")
			mfaGroup.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
			if cfg.SessionManager != nil {
				mfaGroup.Use(middleware.CSRF(cfg.SessionManager))
			}
			{
				mfaGroup.GET("/status", authHandler.GetMFAStatus)
				mfaGroup.POST("/enroll/totp/start", authHandler.StartTOTPEnrollment)
				mfaGroup.POST("/enroll/totp/confirm", authHandler.ConfirmTOTPEnrollment)
				mfaGroup.POST("/enroll/yubiotp/start", authHandler.StartYubiOTPEnrollment)
				mfaGroup.POST("/enroll/webauthn/start", authHandler.StartWebAuthnEnrollment)
				mfaGroup.POST("/enroll/webauthn/confirm", authHandler.ConfirmWebAuthnEnrollment)
				mfaGroup.POST("/verify", authHandler.VerifyMFA)
				mfaGroup.POST("/webauthn/assertion/start", authHandler.StartWebAuthnAssertion)
				mfaGroup.POST("/webauthn/assertion/verify", authHandler.VerifyWebAuthnAssertion)
				mfaGroup.PUT("/preferred-method", authHandler.UpdatePreferredMFAMethod)
				mfaStepUp := mfaGroup.Group("")
				mfaStepUp.Use(middleware.RequireMFAStepUp(cfg.MFASettings))
				mfaStepUp.POST("/backup-codes/regenerate", authHandler.RegenerateBackupCodes)
				mfaStepUp.PUT("/methods/:methodID", authHandler.UpdateMFAMethod)
				mfaStepUp.DELETE("/methods/:methodID", authHandler.DeleteMFAMethod)
				mfaStepUp.DELETE("/trusted-devices/:deviceID", authHandler.RevokeTrustedDevice)
			}
			// User API
			authenticated.GET("/me", apiHandler.GetMe)
			authenticated.GET("/created-by", apiHandler.GetCreatedBy)
			authenticated.GET("/rack", apiHandler.GetRackInfo)
			authenticated.GET("/env", apiHandler.GetEnvValues)
			deployApprovalRequests := authenticated.Group("/deploy-approval-requests")
			{
				deployApprovalRequests.GET("/:id", apiHandler.GetDeployApprovalRequest)
				createDeploy := deployApprovalRequests.Group("")
				createDeploy.Use(middleware.RequireMFAStepUp(cfg.MFASettings))
				createDeploy.POST("", apiHandler.CreateDeployApprovalRequest)
			}
			envMutations := authenticated.Group("")
			if cfg.SessionManager != nil {
				envMutations.Use(middleware.CSRF(cfg.SessionManager))
			}
			envMutations.PUT("/env", apiHandler.UpdateEnvValues)

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
			admin.Use(middleware.CSRF(cfg.SessionManager))
			{
				// Config and settings
				admin.GET("/config", adminHandler.GetConfig)
				admin.PUT("/config", adminHandler.UpdateConfig)
				admin.GET("/settings", adminHandler.GetSettings)
				admin.PUT("/settings/protected_env_vars", adminHandler.UpdateProtectedEnvVars)
				admin.PUT("/settings/approved_commands", adminHandler.UpdateApprovedCommands)
				admin.PUT("/settings/allow_destructive_actions", adminHandler.UpdateAllowDestructiveActions)
				admin.PUT("/settings/mfa", adminHandler.UpdateMFASettings)
				admin.POST("/settings/rack_tls_cert/refresh", adminHandler.RefreshRackTLSCert)
				admin.POST("/diagnostics/sentry", adminHandler.TriggerSentryTest)

				// Users and roles
				admin.GET("/roles", adminHandler.ListRoles)
				admin.GET("/users", adminHandler.ListUsers)
				admin.GET("/users/:email", adminHandler.GetUser)
				admin.POST("/users", adminHandler.CreateUser)
				admin.DELETE("/users/:email", adminHandler.DeleteUser)
				admin.PUT("/users/:email", adminHandler.UpdateUserProfile)
				admin.PUT("/users/:email/roles", adminHandler.UpdateUserRoles)
				admin.GET("/users/:email/sessions", adminHandler.ListUserSessions)
				admin.POST("/users/:email/sessions/:sessionID/revoke", adminHandler.RevokeUserSession)
				admin.POST("/users/:email/sessions/revoke_all", adminHandler.RevokeAllUserSessions)

				// Audit logs
				admin.GET("/audit", adminHandler.ListAuditLogs)
				admin.GET("/audit/export", adminHandler.ExportAuditLogs)

				deployAdmin := admin.Group("/deploy-approval-requests")
				deployAdmin.GET("", adminHandler.ListDeployApprovalRequests)
				deployApprove := deployAdmin.Group("")
				deployApprove.Use(middleware.RequireMFAStepUp(cfg.MFASettings))
				deployApprove.POST("/:id/approve", adminHandler.ApproveDeployApprovalRequest)
				deployApprove.POST("/:id/reject", adminHandler.RejectDeployApprovalRequest)

				// API tokens (rate limit creation)
				tokenGroup := admin.Group("/tokens")
				tokenGroup.GET("", adminHandler.ListAPITokens)
				tokenGroup.GET("/permissions", adminHandler.GetTokenPermissionMetadata)
				tokenGroup.GET("/:tokenID", adminHandler.GetAPIToken)

				tokenSensitive := tokenGroup.Group("")
				tokenSensitive.Use(middleware.RequireMFAStepUp(cfg.MFASettings))
				tokenSensitive.POST("", middleware.RateLimit(cfg.Config, cfg.SecurityNotifier), adminHandler.CreateAPIToken)
				tokenSensitive.PUT("/:tokenID", adminHandler.UpdateAPIToken)
				tokenSensitive.DELETE("/:tokenID", adminHandler.DeleteAPIToken)
			}
		}
	}

	// Catch-all: Proxy to Convox (CLI only, no cookie auth)
	router.NoRoute(
		middleware.CLIOnly(cfg.AuthService),
		middleware.RequireMFAEnrollment(cfg.Database, cfg.MFASettings),
		proxyHandler.ProxyToRack,
	)
}
