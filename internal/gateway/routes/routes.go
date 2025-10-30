package routes

import (
	"net/http"
	"strings"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/openapi"
)

// Config holds dependencies needed for route setup
type Config struct {
	*deps.Gateway
	App interface{} // Reference to app for handlers that need it
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
	router.Use(middleware.RequestLogger(cfg.AuditLogger, cfg.DefaultRack, cfg.Config.DevMode))
	router.Use(middleware.DebugLogging(cfg.Config))
	router.Use(middleware.SecurityHeaders(cfg.Config))
	router.Use(middleware.HostValidator(cfg.Config))
	router.Use(middleware.OriginValidator(cfg.Config))
	router.Use(gin.Recovery())

	// Capture all 500-level errors to Sentry (must be after sentrygin middleware)
	if cfg.SentryEnabled {
		router.Use(middleware.SentryErrorCapture())
	}

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/.well-known/") {
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	})

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
	corsConfig.AllowHeaders = append(corsConfig.AllowHeaders, "X-CSRF-Token", "Authorization", "X-Mfa-Totp", "X-MFA-WebAuthn")
	router.Use(cors.New(corsConfig))

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(cfg.OAuthHandler, cfg.Database, cfg.Config, cfg.SessionManager, cfg.MFAService, cfg.MFASettings, cfg.SecurityNotifier, cfg.AuditLogger)
	apiHandler := handlers.NewAPIHandler(cfg.RBACManager, cfg.Database, cfg.Config, cfg.RackCertManager, cfg.MFASettings, cfg.AuditLogger, cfg.SettingsService, cfg.SlackNotifier, cfg.JobsClient)
	adminHandler := handlers.NewAdminHandler(cfg.RBACManager, cfg.Database, cfg.TokenService, cfg.EmailSender, cfg.Config, cfg.RackCertManager, cfg.SessionManager, cfg.MFASettings, cfg.AuditLogger, cfg.SettingsService, cfg.JobsClient)
	settingsHandler := handlers.NewSettingsHandler(cfg.SettingsService, cfg.RBACManager)
	proxyHandler := handlers.NewProxyHandler(cfg.ProxyHandler)
	staticHandler := handlers.NewStaticHandler(cfg.Config, cfg.SessionManager)
	healthHandler := handlers.NewHealthHandler()

	// Root redirect
	router.GET("/", handlers.RootRedirect)

	// Static files
	router.GET("/favicon.ico", handlers.Favicon)
	router.GET("/robots.txt", handlers.Robots)

	// Web UI static files
	router.GET("/app", handlers.WebRedirect)
	router.GET("/app/*filepath", staticHandler.ServeStatic)

	// API documentation
	openapi.Register(router)

	// API routes
	api := router.Group("/api/v1")
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
		authenticated.Use(middleware.EnforceMFARequirements(cfg.MFAService, cfg.Database, cfg.MFASettings))
		{
			mfaGroup := authenticated.Group("/auth/mfa")
			mfaGroup.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
			if cfg.SessionManager != nil {
				mfaGroup.Use(middleware.CSRF(cfg.SessionManager))
			}
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
			mfaGroup.PUT("/methods/:methodID", authHandler.UpdateMFAMethod)
			mfaGroup.POST("/backup-codes/regenerate", authHandler.RegenerateBackupCodes)
			mfaGroup.DELETE("/methods/:methodID", authHandler.DeleteMFAMethod)
			mfaGroup.POST("/trusted-devices/trust", authHandler.TrustCurrentDevice)
			mfaGroup.DELETE("/trusted-devices/:deviceID", authHandler.RevokeTrustedDevice)
			// User API
			authenticated.GET("/info", apiHandler.GetInfo)
			authenticated.GET("/created-by", apiHandler.GetCreatedBy)
			authenticated.GET("/rack", apiHandler.GetRackInfo)

			// Environment management
			apps := authenticated.Group("/apps/:app")
			{
				apps.GET("/env", apiHandler.GetEnvValues)

				envMutations := apps.Group("")
				if cfg.SessionManager != nil {
					envMutations.Use(middleware.CSRF(cfg.SessionManager))
				}
				envMutations.PUT("/env", apiHandler.UpdateEnvValues)

				apps.GET("/settings", settingsHandler.GetAllAppSettings)

				appSettings := apps.Group("/settings")
				if cfg.SessionManager != nil {
					appSettings.Use(middleware.CSRF(cfg.SessionManager))
				}
				appSettings.PUT("/vcs-ci-deploy", settingsHandler.UpdateAppVCSCIDeploySettings)
				appSettings.DELETE("/vcs-ci-deploy", settingsHandler.DeleteAppVCSCIDeploySettings)
				appSettings.PUT("/protected-env-vars", settingsHandler.UpdateAppProtectedEnvVars)
				appSettings.DELETE("/protected-env-vars", settingsHandler.DeleteAppProtectedEnvVars)
				appSettings.PUT("/secret-env-vars", settingsHandler.UpdateAppSecretEnvVars)
				appSettings.DELETE("/secret-env-vars", settingsHandler.DeleteAppSecretEnvVars)
				appSettings.PUT("/approved-deploy-commands", settingsHandler.UpdateAppApprovedDeployCommands)
				appSettings.DELETE("/approved-deploy-commands", settingsHandler.DeleteAppApprovedDeployCommands)
				appSettings.PUT("/service-image-patterns", settingsHandler.UpdateAppServiceImagePatterns)
				appSettings.DELETE("/service-image-patterns", settingsHandler.DeleteAppServiceImagePatterns)
			}

			// Deploy approval requests
			deployApprovalRequests := authenticated.Group("/deploy-approval-requests")
			{
				deployApprovalRequests.GET("", adminHandler.ListDeployApprovalRequests)
				deployApprovalRequests.GET("/:id/audit-logs", adminHandler.GetDeployApprovalRequestAuditLogs)
				deployApprovalRequests.GET("/:id", apiHandler.GetDeployApprovalRequest)

				deployApprove := deployApprovalRequests.Group("")
				if cfg.SessionManager != nil {
					deployApprove.Use(middleware.CSRF(cfg.SessionManager))
				}
				deployApprove.POST("", apiHandler.CreateDeployApprovalRequest)
				deployApprove.POST("/:id/approve", adminHandler.ApproveDeployApprovalRequest)
				deployApprove.POST("/:id/reject", adminHandler.RejectDeployApprovalRequest)
			}

			// Convox proxy endpoints (safe GET only for web UI)
			convox := authenticated.Group("/convox")
			{
				convox.GET("/apps", proxyHandler.ProxyStripPrefix)
				convox.GET("/apps/*path", proxyHandler.ProxyStripPrefix)
				convox.GET("/instances", proxyHandler.ProxyStripPrefix)
				convox.GET("/system/processes", proxyHandler.ProxyStripPrefix)
			}

			// Global settings
			authenticated.GET("/settings", settingsHandler.GetAllGlobalSettings)
			settingsMutations := authenticated.Group("/settings")
			if cfg.SessionManager != nil {
				settingsMutations.Use(middleware.CSRF(cfg.SessionManager))
			}
			settingsMutations.PUT("/mfa-configuration", settingsHandler.UpdateGlobalMFAConfiguration)
			settingsMutations.DELETE("/mfa-configuration", settingsHandler.DeleteGlobalMFAConfiguration)
			settingsMutations.PUT("/allow-destructive-actions", settingsHandler.UpdateGlobalAllowDestructiveActions)
			settingsMutations.DELETE("/allow-destructive-actions", settingsHandler.DeleteGlobalAllowDestructiveActions)
			settingsMutations.PUT("/vcs-and-ci-defaults", settingsHandler.UpdateGlobalVCSAndCIDefaults)
			settingsMutations.DELETE("/vcs-and-ci-defaults", settingsHandler.DeleteGlobalVCSAndCIDefaults)
			settingsMutations.PUT("/deploy-approvals", settingsHandler.UpdateGlobalDeployApprovals)
			settingsMutations.DELETE("/deploy-approvals", settingsHandler.DeleteGlobalDeployApprovals)
			settingsMutations.POST("/rack-tls-cert/refresh", adminHandler.RefreshRackTLSCert)

			diagnostics := authenticated.Group("/diagnostics")
			if cfg.SessionManager != nil {
				diagnostics.Use(middleware.CSRF(cfg.SessionManager))
			}
			diagnostics.POST("/sentry", adminHandler.TriggerSentryTest)

			// Roles and users
			authenticated.GET("/roles", adminHandler.ListRoles)

			users := authenticated.Group("/users")
			if cfg.SessionManager != nil {
				users.Use(middleware.CSRF(cfg.SessionManager))
			}
			users.GET("", adminHandler.ListUsers)
			users.GET("/:email", adminHandler.GetUser)
			users.POST("", adminHandler.CreateUser)
			users.DELETE("/:email", adminHandler.DeleteUser)
			users.PUT("/:email", adminHandler.UpdateUser)
			users.PUT("/:email/name", adminHandler.UpdateUserName)
			users.GET("/:email/sessions", adminHandler.ListUserSessions)
			users.POST("/:email/sessions/:sessionID/revoke", adminHandler.RevokeUserSession)
			users.POST("/:email/sessions/revoke_all", adminHandler.RevokeAllUserSessions)
			users.POST("/:email/lock", adminHandler.LockUser)
			users.POST("/:email/unlock", adminHandler.UnlockUser)

			// Audit logs
			auditLogs := authenticated.Group("/audit-logs")
			if cfg.SessionManager != nil {
				auditLogs.Use(middleware.CSRF(cfg.SessionManager))
			}
			auditLogs.GET("", adminHandler.ListAuditLogs)
			auditLogs.GET("/export", adminHandler.ExportAuditLogs)

			// API tokens (rate limit creation)
			apiTokens := authenticated.Group("/api-tokens")
			if cfg.SessionManager != nil {
				apiTokens.Use(middleware.CSRF(cfg.SessionManager))
			}
			apiTokens.GET("", adminHandler.ListAPITokens)
			apiTokens.GET("/permissions", adminHandler.GetTokenPermissionMetadata)
			apiTokens.GET("/:tokenID", adminHandler.GetAPIToken)

			apiTokenSensitive := apiTokens.Group("")
			apiTokenSensitive.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
			apiTokenSensitive.POST("", adminHandler.CreateAPIToken)
			apiTokens.PUT("/:tokenID", adminHandler.UpdateAPIToken)
			apiTokens.DELETE("/:tokenID", adminHandler.DeleteAPIToken)

			// Background jobs
			jobs := authenticated.Group("/jobs")
			if cfg.SessionManager != nil {
				jobs.Use(middleware.CSRF(cfg.SessionManager))
			}
			jobs.GET("", adminHandler.ListJobs)
			jobs.GET("/:id", adminHandler.GetJob)

			// Slack integration
			integrations := authenticated.Group("/integrations")
			slack := integrations.Group("/slack")
			if cfg.SessionManager != nil {
				slack.Use(middleware.CSRF(cfg.SessionManager))
			}
			slack.GET("", adminHandler.GetSlackIntegrationHandler)
			slack.POST("/oauth/authorize", adminHandler.SlackOAuthAuthorizeHandler)
			slack.GET("/oauth/callback", adminHandler.SlackOAuthCallbackHandler)
			slack.PUT("/channels", adminHandler.UpdateSlackChannelsHandler)
			slack.DELETE("", adminHandler.DeleteSlackIntegrationHandler)
			slack.GET("/channels/list", adminHandler.ListSlackChannelsHandler)
			slack.POST("/test", adminHandler.TestSlackNotificationHandler)
			slack.PUT("/alerts", adminHandler.UpdateSlackAlertSettingsHandler)
		}

		convoxCLI := api.Group("/rack-proxy")
		convoxCLI.Use(middleware.CLIOnly(cfg.AuthService))
		convoxCLI.Use(middleware.RequireMFAEnrollment(cfg.Database, cfg.MFASettings))
		{
			// Proxy all methods to Convox rack, stripping /api/v1/rack-proxy prefix
			convoxCLI.GET("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.POST("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.PUT("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.PATCH("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.DELETE("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.HEAD("/*path", proxyHandler.ProxyStripPrefix)
			convoxCLI.OPTIONS("/*path", proxyHandler.ProxyStripPrefix)
		}
	}
}
