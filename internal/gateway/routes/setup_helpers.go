package routes

import (
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
)

func setupGlobalMiddleware(router *gin.Engine, cfg *Config) {
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

	if cfg.SentryEnabled {
		router.Use(middleware.SentryErrorCapture())
	}
}

func setupCORS(router *gin.Engine, cfg *Config) {
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
	corsConfig.AllowHeaders = append(
		corsConfig.AllowHeaders,
		"X-CSRF-Token",
		"Authorization",
		"X-Mfa-Totp",
		"X-MFA-WebAuthn",
	)
	router.Use(cors.New(corsConfig))
}

type handlerSet struct {
	auth     *handlers.AuthHandler
	api      *handlers.APIHandler
	admin    *handlers.AdminHandler
	settings *handlers.SettingsHandler
	proxy    *handlers.ProxyHandler
	static   *handlers.StaticHandler
	health   *handlers.HealthHandler
}

func initializeHandlers(cfg *Config) *handlerSet {
	return &handlerSet{
		auth: handlers.NewAuthHandler(
			cfg.OAuthHandler,
			cfg.Database,
			cfg.Config,
			cfg.SessionManager,
			cfg.MFAService,
			cfg.MFASettings,
			cfg.SecurityNotifier,
			cfg.AuditLogger,
		),
		api: handlers.NewAPIHandler(
			cfg.RBACManager,
			cfg.Database,
			cfg.Config,
			cfg.RackCertManager,
			cfg.MFASettings,
			cfg.AuditLogger,
			cfg.SettingsService,
			cfg.SlackNotifier,
			cfg.JobsClient,
		),
		admin: handlers.NewAdminHandler(
			cfg.RBACManager,
			cfg.Database,
			cfg.TokenService,
			cfg.EmailSender,
			cfg.Config,
			cfg.RackCertManager,
			cfg.SessionManager,
			cfg.MFASettings,
			cfg.AuditLogger,
			cfg.SettingsService,
			cfg.JobsClient,
		),
		settings: handlers.NewSettingsHandler(cfg.SettingsService, cfg.RBACManager),
		proxy:    handlers.NewProxyHandler(cfg.ProxyHandler),
		static:   handlers.NewStaticHandler(cfg.Config, cfg.SessionManager),
		health:   handlers.NewHealthHandler(),
	}
}
