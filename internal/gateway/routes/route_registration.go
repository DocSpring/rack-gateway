package routes

import (
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/openapi"
)

func registerStaticRoutes(router *gin.Engine, h *handlerSet) {
	router.GET("/", handlers.RootRedirect)
	router.GET("/favicon.ico", handlers.Favicon)
	router.GET("/robots.txt", handlers.Robots)
	router.GET("/app", handlers.WebRedirect)
	router.GET("/app/*filepath", h.static.ServeStatic)
	openapi.Register(router)
}

func registerAuthRoutes(api *gin.RouterGroup, cfg *Config, h *handlerSet) {
	authGroup := api.Group("")
	authGroup.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
	authGroup.POST("/auth/cli/start", h.auth.CLILoginStart)
	authGroup.GET("/auth/cli/callback", h.auth.CLILoginCallback)
	authGroup.POST("/auth/cli/complete", h.auth.CLILoginComplete)
	authGroup.GET("/auth/cli/mfa", h.auth.CLILoginMFAForm)
	authGroup.POST("/auth/cli/mfa", h.auth.CLILoginMFASubmit)
	authGroup.GET("/auth/web/login", h.auth.WebLoginStart)
	authGroup.HEAD("/auth/web/login", h.auth.WebLoginStart)
	authGroup.GET("/auth/web/callback", h.auth.WebLoginCallback)
	authGroup.GET("/auth/web/logout", h.auth.WebLogout)
}

func registerMFARoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	mfaGroup := authenticated.Group("/auth/mfa")
	mfaGroup.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
	if cfg.SessionManager != nil {
		mfaGroup.Use(middleware.CSRF(cfg.SessionManager))
	}
	mfaGroup.GET("/status", h.auth.GetMFAStatus)
	mfaGroup.POST("/enroll/totp/start", h.auth.StartTOTPEnrollment)
	mfaGroup.POST("/enroll/totp/confirm", h.auth.ConfirmTOTPEnrollment)
	mfaGroup.POST("/enroll/yubiotp/start", h.auth.StartYubiOTPEnrollment)
	mfaGroup.POST("/enroll/webauthn/start", h.auth.StartWebAuthnEnrollment)
	mfaGroup.POST("/enroll/webauthn/confirm", h.auth.ConfirmWebAuthnEnrollment)
	mfaGroup.POST("/verify", h.auth.VerifyMFA)
	mfaGroup.POST("/webauthn/assertion/start", h.auth.StartWebAuthnAssertion)
	mfaGroup.POST("/webauthn/assertion/verify", h.auth.VerifyWebAuthnAssertion)
	mfaGroup.PUT("/preferred-method", h.auth.UpdatePreferredMFAMethod)
	mfaGroup.PUT("/methods/:methodID", h.auth.UpdateMFAMethod)
	mfaGroup.POST("/backup-codes/regenerate", h.auth.RegenerateBackupCodes)
	mfaGroup.DELETE("/methods/:methodID", h.auth.DeleteMFAMethod)
	mfaGroup.POST("/trusted-devices/trust", h.auth.TrustCurrentDevice)
	mfaGroup.DELETE("/trusted-devices/:deviceID", h.auth.RevokeTrustedDevice)
}

func registerAPIRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	authenticated.GET("/info", h.api.GetInfo)
	authenticated.GET("/created-by", h.api.GetCreatedBy)
	authenticated.GET("/rack", h.api.GetRackInfo)

	apps := authenticated.Group("/apps/:app")
	apps.GET("/env", h.api.GetEnvValues)

	envMutations := apps.Group("")
	if cfg.SessionManager != nil {
		envMutations.Use(middleware.CSRF(cfg.SessionManager))
	}
	envMutations.PUT("/env", h.api.UpdateEnvValues)

	apps.GET("/settings", h.settings.GetAllAppSettings)

	appSettings := apps.Group("/settings")
	if cfg.SessionManager != nil {
		appSettings.Use(middleware.CSRF(cfg.SessionManager))
	}
	appSettings.PUT("/vcs-ci-deploy", h.settings.UpdateAppVCSCIDeploySettings)
	appSettings.DELETE("/vcs-ci-deploy", h.settings.DeleteAppVCSCIDeploySettings)
	appSettings.PUT("/protected-env-vars", h.settings.UpdateAppProtectedEnvVars)
	appSettings.DELETE("/protected-env-vars", h.settings.DeleteAppProtectedEnvVars)
	appSettings.PUT("/secret-env-vars", h.settings.UpdateAppSecretEnvVars)
	appSettings.DELETE("/secret-env-vars", h.settings.DeleteAppSecretEnvVars)
	appSettings.PUT("/approved-deploy-commands", h.settings.UpdateAppApprovedDeployCommands)
	appSettings.DELETE("/approved-deploy-commands", h.settings.DeleteAppApprovedDeployCommands)
	appSettings.PUT("/service-image-patterns", h.settings.UpdateAppServiceImagePatterns)
	appSettings.DELETE("/service-image-patterns", h.settings.DeleteAppServiceImagePatterns)

	convox := authenticated.Group("/convox")
	convox.GET("/apps", h.proxy.ProxyStripPrefix)
	convox.GET("/apps/*path", h.proxy.ProxyStripPrefix)
	convox.GET("/instances", h.proxy.ProxyStripPrefix)
	convox.GET("/system/processes", h.proxy.ProxyStripPrefix)
}

func registerDeployApprovalRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	deployApprovalRequests := authenticated.Group("/deploy-approval-requests")
	deployApprovalRequests.GET("", h.admin.ListDeployApprovalRequests)
	deployApprovalRequests.GET("/:id/audit-logs", h.admin.GetDeployApprovalRequestAuditLogs)
	deployApprovalRequests.GET("/:id", h.api.GetDeployApprovalRequest)

	deployApprove := deployApprovalRequests.Group("")
	if cfg.SessionManager != nil {
		deployApprove.Use(middleware.CSRF(cfg.SessionManager))
	}
	deployApprove.POST("", h.api.CreateDeployApprovalRequest)
	deployApprove.POST("/:id/approve", h.admin.ApproveDeployApprovalRequest)
	deployApprove.POST("/:id/reject", h.admin.RejectDeployApprovalRequest)
	deployApprove.POST("/:id/extend", h.admin.ExtendDeployApprovalRequest)
}

func registerGlobalSettingsRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	authenticated.GET("/settings", h.settings.GetAllGlobalSettings)
	settingsMutations := authenticated.Group("/settings")
	if cfg.SessionManager != nil {
		settingsMutations.Use(middleware.CSRF(cfg.SessionManager))
	}
	settingsMutations.PUT("/mfa-configuration", h.settings.UpdateGlobalMFAConfiguration)
	settingsMutations.DELETE("/mfa-configuration", h.settings.DeleteGlobalMFAConfiguration)
	settingsMutations.PUT("/allow-destructive-actions", h.settings.UpdateGlobalAllowDestructiveActions)
	settingsMutations.DELETE("/allow-destructive-actions", h.settings.DeleteGlobalAllowDestructiveActions)
	settingsMutations.PUT("/vcs-and-ci-defaults", h.settings.UpdateGlobalVCSAndCIDefaults)
	settingsMutations.DELETE("/vcs-and-ci-defaults", h.settings.DeleteGlobalVCSAndCIDefaults)
	settingsMutations.PUT("/deploy-approvals", h.settings.UpdateGlobalDeployApprovals)
	settingsMutations.DELETE("/deploy-approvals", h.settings.DeleteGlobalDeployApprovals)
	settingsMutations.POST("/rack-tls-cert/refresh", h.admin.RefreshRackTLSCert)

	diagnostics := authenticated.Group("/diagnostics")
	if cfg.SessionManager != nil {
		diagnostics.Use(middleware.CSRF(cfg.SessionManager))
	}
	diagnostics.POST("/sentry", h.admin.TriggerSentryTest)
}

func registerUserManagementRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	authenticated.GET("/roles", h.admin.ListRoles)

	users := authenticated.Group("/users")
	if cfg.SessionManager != nil {
		users.Use(middleware.CSRF(cfg.SessionManager))
	}
	users.GET("", h.admin.ListUsers)
	users.GET("/:email", h.admin.GetUser)
	users.POST("", h.admin.CreateUser)
	users.DELETE("/:email", h.admin.DeleteUser)
	users.PUT("/:email", h.admin.UpdateUser)
	users.PUT("/:email/name", h.admin.UpdateUserName)
	users.GET("/:email/sessions", h.admin.ListUserSessions)
	users.POST("/:email/sessions/:sessionID/revoke", h.admin.RevokeUserSession)
	users.POST("/:email/sessions/revoke_all", h.admin.RevokeAllUserSessions)
	users.POST("/:email/lock", h.admin.LockUser)
	users.POST("/:email/unlock", h.admin.UnlockUser)

	auditLogs := authenticated.Group("/audit-logs")
	if cfg.SessionManager != nil {
		auditLogs.Use(middleware.CSRF(cfg.SessionManager))
	}
	auditLogs.GET("", h.admin.ListAuditLogs)
	auditLogs.GET("/export", h.admin.ExportAuditLogs)

	apiTokens := authenticated.Group("/api-tokens")
	if cfg.SessionManager != nil {
		apiTokens.Use(middleware.CSRF(cfg.SessionManager))
	}
	apiTokens.GET("", h.admin.ListAPITokens)
	apiTokens.GET("/permissions", h.admin.GetTokenPermissionMetadata)
	apiTokens.GET("/:tokenID", h.admin.GetAPIToken)

	apiTokenSensitive := apiTokens.Group("")
	apiTokenSensitive.Use(middleware.RateLimit(cfg.Config, cfg.SecurityNotifier))
	apiTokenSensitive.POST("", h.admin.CreateAPIToken)
	apiTokens.PUT("/:tokenID", h.admin.UpdateAPIToken)
	apiTokens.DELETE("/:tokenID", h.admin.DeleteAPIToken)

	jobs := authenticated.Group("/jobs")
	if cfg.SessionManager != nil {
		jobs.Use(middleware.CSRF(cfg.SessionManager))
	}
	jobs.GET("", h.admin.ListJobs)
	jobs.GET("/:id", h.admin.GetJob)
	jobs.DELETE("/:id", h.admin.DeleteJob)
	jobs.POST("/:id/retry", h.admin.RetryJob)
}

func registerIntegrationRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	integrations := authenticated.Group("/integrations")
	slack := integrations.Group("/slack")
	if cfg.SessionManager != nil {
		slack.Use(middleware.CSRF(cfg.SessionManager))
	}
	slack.GET("", h.admin.GetSlackIntegrationHandler)
	slack.POST("/oauth/authorize", h.admin.SlackOAuthAuthorizeHandler)
	slack.GET("/oauth/callback", h.admin.SlackOAuthCallbackHandler)
	slack.PUT("/channels", h.admin.UpdateSlackChannelsHandler)
	slack.DELETE("", h.admin.DeleteSlackIntegrationHandler)
	slack.GET("/channels/list", h.admin.ListSlackChannelsHandler)
	slack.POST("/test", h.admin.TestSlackNotificationHandler)
	slack.PUT("/alerts", h.admin.UpdateSlackAlertSettingsHandler)
}

func registerAdminRoutes(authenticated *gin.RouterGroup, cfg *Config, h *handlerSet) {
	registerDeployApprovalRoutes(authenticated, cfg, h)
	registerGlobalSettingsRoutes(authenticated, cfg, h)
	registerUserManagementRoutes(authenticated, cfg, h)
	registerIntegrationRoutes(authenticated, cfg, h)
}

func registerCLIProxyRoutes(api *gin.RouterGroup, cfg *Config, h *handlerSet) {
	convoxCLI := api.Group("/rack-proxy")
	convoxCLI.Use(middleware.CLIOnly(cfg.AuthService))
	convoxCLI.Use(middleware.RequireMFAEnrollment(cfg.Database, cfg.MFASettings))
	convoxCLI.GET("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.POST("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.PUT("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.PATCH("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.DELETE("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.HEAD("/*path", h.proxy.ProxyStripPrefix)
	convoxCLI.OPTIONS("/*path", h.proxy.ProxyStripPrefix)
}
