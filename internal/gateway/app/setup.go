package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/asyncmail"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/proxy"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routes"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	slackpkg "github.com/DocSpring/rack-gateway/internal/gateway/slack"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

type mfaRuntimeConfig struct {
	issuer         string
	yubiClientID   string
	yubiSecretKey  string
	webAuthnRPID   string
	webAuthnOrigin string
}

// initializeServices sets up all application services (matching original main.go exactly)
func (a *App) initializeServices() error {
	if err := a.ensureAuditSecret(); err != nil {
		return err
	}
	if err := a.seedInitialUsers(); err != nil {
		return err
	}

	if err := a.initSettingsService(); err != nil {
		return err
	}
	a.initSessionManager()

	mfaCfg, err := a.initMFAConfig()
	if err != nil {
		return err
	}

	if err := a.initRBACManager(); err != nil {
		return err
	}

	a.initTokenAndAuthServices()

	redirectInput, issuerURL, allowedDomain, err := a.resolveOAuthParameters()
	if err != nil {
		return err
	}
	if err := a.initOAuthHandler(redirectInput, issuerURL, allowedDomain); err != nil {
		return err
	}

	auditLogger := a.initAuditLogger()
	slackNotifier := a.initSlackNotifier(auditLogger)

	a.initRackCertManager()

	deliverySender := email.NewIdentifiedSender(a.newEmailSender(), a.Config.GatewayIdentity())
	if err := a.initJobsClient(deliverySender, slackNotifier, auditLogger); err != nil {
		return err
	}

	if err := a.initMFAService(mfaCfg); err != nil {
		return err
	}

	a.configureAuditEnqueuer(auditLogger)
	if err := a.startJobsWorker(); err != nil {
		return err
	}
	a.initSecurityNotifier(auditLogger)

	rackName, rackAlias := a.deriveRackIdentity()
	a.initProxyHandler(auditLogger, rackName, rackAlias)

	return nil
}

func (a *App) ensureAuditSecret() error {
	if strings.TrimSpace(os.Getenv("AUDIT_HMAC_SECRET")) == "" && !a.Config.DevMode {
		return fmt.Errorf("AUDIT_HMAC_SECRET must be set in non-dev environments")
	}
	return nil
}

// seedInitialUsers creates the seed lists' missing users, audit-logged in the same transaction. The gateway doesn't
// start if they can't be recorded.
func (a *App) seedInitialUsers() error {
	events, err := a.Database.SeedDatabase(&db.SeedConfig{
		AdminUsers:      a.Config.AdminUsers,
		ViewerUsers:     a.Config.ViewerUsers,
		DeployerUsers:   a.Config.DeployerUsers,
		OperationsUsers: a.Config.OperationsUsers,
	}, audit.SeededUserEntry)
	audit.RecordedInTransaction(events, err)
	if err != nil {
		return fmt.Errorf("failed to seed users: %w", err)
	}
	return nil
}

// initSettingsService refuses to start on an invalid setting environment variable, and logs stored settings that
// an environment variable now overrides.
func (a *App) initSettingsService() error {
	if err := settings.ValidateEnvironment(os.Environ()); err != nil {
		return err
	}
	a.SettingsService = settings.NewService(a.Database)
	shadowed, err := a.SettingsService.ShadowedSettings()
	if err != nil {
		return fmt.Errorf("failed to check stored settings: %w", err)
	}
	for _, entry := range shadowed {
		log.Printf("settings: the stored value of %s has no effect; delete it", entry)
	}
	return nil
}

func (a *App) initSessionManager() {
	a.SessionManager = auth.NewSessionManager(a.Database, a.Config.SessionSecret, a.SettingsService)
}

func (a *App) initMFAConfig() (*mfaRuntimeConfig, error) {
	// The MFA settings are read for each request that needs them; loading them now refuses to start with a setting
	// (e.g. an environment variable) that can't be parsed.
	if _, err := a.SettingsService.GetMFASettings(); err != nil {
		return nil, fmt.Errorf("failed to load MFA settings: %w", err)
	}

	cfg := &mfaRuntimeConfig{
		issuer:        deriveMFAIssuer(),
		yubiClientID:  strings.TrimSpace(os.Getenv("YUBICO_CLIENT_ID")),
		yubiSecretKey: strings.TrimSpace(os.Getenv("YUBICO_SECRET_KEY")),
	}

	cfg.webAuthnRPID, cfg.webAuthnOrigin = a.resolveWebAuthnConfig()
	logWebAuthnStatus(cfg)

	return cfg, nil
}

func deriveMFAIssuer() string {
	if enforced := strings.TrimSpace(os.Getenv("RACK_ALIAS")); enforced != "" {
		return fmt.Sprintf("Rack Gateway (%s)", enforced)
	}
	if enforced := strings.TrimSpace(os.Getenv("RACK")); enforced != "" {
		return fmt.Sprintf("Rack Gateway (%s)", enforced)
	}
	return "Rack Gateway"
}

func (a *App) resolveWebAuthnConfig() (string, string) {
	rpid := strings.TrimSpace(os.Getenv("WEBAUTHN_RP_ID"))
	origin := strings.TrimSpace(os.Getenv("WEBAUTHN_ORIGIN"))

	if rpid == "" && a.Config.Domain != "" {
		rpid = a.Config.Domain
	}

	if origin != "" {
		return rpid, origin
	}
	return rpid, a.Config.PublicURL()
}

func logWebAuthnStatus(cfg *mfaRuntimeConfig) {
	if cfg.webAuthnRPID != "" && cfg.webAuthnOrigin != "" {
		log.Printf("WebAuthn enabled: rpid=%s origin=%s", cfg.webAuthnRPID, cfg.webAuthnOrigin)
		return
	}
	log.Printf("WebAuthn disabled (no RP ID or origin configured)")
}

func (a *App) initRBACManager() error {
	manager, err := rbac.NewDBManager(a.Database, a.Config.GoogleAllowedDomain)
	if err != nil {
		return fmt.Errorf("failed to initialize RBAC: %w", err)
	}
	a.RBACManager = manager
	return nil
}

func (a *App) initTokenAndAuthServices() {
	a.TokenService = token.NewService(a.Database)
	a.AuthService = auth.NewAuthService(a.TokenService, a.Database, a.SessionManager)

	log.Printf("Environment PORT=%s, Config Port=%s",
		logutil.SanitizeForLog(os.Getenv("PORT")), logutil.SanitizeForLog(a.Config.Port))
	log.Printf("OAuth config - ClientID: %s, BaseURL: %s",
		logutil.SanitizeForLog(a.Config.GoogleClientID), logutil.SanitizeForLog(a.Config.GoogleOAuthBaseURL))
}

func (a *App) resolveOAuthParameters() (string, string, string, error) {
	issuerURL := a.Config.GoogleOAuthBaseURL
	if issuerURL == "" {
		issuerURL = "https://accounts.google.com"
	}

	redirectInput := a.Config.PublicURL()
	if redirectInput == "" {
		return "", "", "", fmt.Errorf("DOMAIN must be set (or use DEV_MODE with PORT) to derive OAuth redirect URLs")
	}

	return redirectInput, issuerURL, a.Config.GoogleAllowedDomain, nil
}

func (a *App) initOAuthHandler(redirectInput, issuerURL, allowedDomain string) error {
	handler, err := auth.NewOAuthHandler(
		a.Config.GoogleClientID,
		a.Config.GoogleClientSecret,
		redirectInput,
		allowedDomain,
		issuerURL,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize OAuth handler: %w", err)
	}
	a.OAuthHandler = handler
	return nil
}

func (a *App) initAuditLogger() *audit.Logger {
	logger := audit.NewLogger(a.Database)
	a.AuditLogger = logger
	return logger
}

func (a *App) initSlackNotifier(logger *audit.Logger) *slackpkg.Notifier {
	notifier := slackpkg.NewNotifier(a.Database, a.Config.GatewayIdentity())
	logger.SetSlackNotifier(notifier)
	a.SlackNotifier = notifier
	return notifier
}

func (a *App) initRackCertManager() {
	if !a.Config.RackTLSPinningEnabled {
		return
	}
	a.RackCertManager = rackcert.NewManager(a.Config, a.Database)
	if _, err := a.RackCertManager.TLSConfig(context.Background()); err != nil {
		log.Printf("Warning: failed to initialize rack TLS certificate: %v", err)
	}
}

func (a *App) newEmailSender() email.Sender {
	pmToken := os.Getenv("POSTMARK_API_TOKEN")
	from := os.Getenv("POSTMARK_FROM")
	if from == "" {
		domain := a.Config.GoogleAllowedDomain
		if domain == "" {
			domain = "localhost"
		}
		from = "no-reply@" + domain
	}
	pmStream := os.Getenv("POSTMARK_STREAM")
	return email.NewSender(pmToken, from, pmStream)
}

func (a *App) initJobsClient(sender email.Sender, notifier *slackpkg.Notifier, auditLogger *audit.Logger) error {
	// Load WORM config if environment variables are set
	auditAnchorConfig, err := jobs.NewAuditAnchorConfigFromEnv()
	if err != nil {
		return fmt.Errorf("failed to load audit anchor config: %w", err)
	}
	// Anchors are only as permanent as the bucket's Object Lock rule: refuse to start without one that keeps them.
	if auditAnchorConfig != nil {
		if err := auditAnchorConfig.CheckBucketRetention(context.Background()); err != nil {
			return fmt.Errorf("audit anchor bucket: %w", err)
		}
	}

	client, err := jobs.NewClient(a.Database.Pool(), &jobs.Dependencies{
		Database:      a.Database,
		EmailSender:   sender,
		SlackNotifier: notifier,
		AuditLogger:   auditLogger,
		CircleCIToken: a.Config.CircleCIToken,
		GitHubToken:   a.Config.GitHubToken,
		GitHubAPIURL:  a.Config.GitHubAPIURL,

		DeployApprovalPendingMaxAge: a.Config.DeployApprovalPendingMaxAge,
	}, auditAnchorConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize jobs client: %w", err)
	}
	a.JobsClient = client
	a.EmailSender = asyncmail.NewSender(client)
	return nil
}

func (a *App) initMFAService(cfg *mfaRuntimeConfig) error {
	service, err := mfa.NewService(
		a.Database,
		cfg.issuer,
		a.SettingsService,
		[]byte(a.Config.SessionSecret),
		cfg.yubiClientID,
		cfg.yubiSecretKey,
		cfg.webAuthnRPID,
		cfg.webAuthnOrigin,
		a.EmailSender,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize MFA service: %w", err)
	}
	a.MFAService = service
	return nil
}

func (a *App) configureAuditEnqueuer(logger *audit.Logger) {
	logger.SetAuditEventEnqueuer(jobs.NewAuditEventEnqueuer(a.JobsClient))
}

func (a *App) startJobsWorker() error {
	if err := a.JobsClient.Start(context.Background()); err != nil {
		return fmt.Errorf("start jobs worker: %w", err)
	}
	log.Printf("Jobs worker started")
	return nil
}

func (a *App) initSecurityNotifier(logger *audit.Logger) {
	a.SecurityNotifier = security.NewNotifier(logger, a.Database, a.JobsClient)
}

func (a *App) deriveRackIdentity() (string, string) {
	rackName := strings.TrimSpace(os.Getenv("RACK"))
	if rackName == "" {
		rackName = a.rackNameFromConfig()
	}

	rackAlias := strings.TrimSpace(os.Getenv("RACK_ALIAS"))
	if rackAlias == "" {
		rackAlias = rackName
	}
	return rackName, rackAlias
}

func (a *App) rackNameFromConfig() string {
	if rc, ok := a.Config.Racks["default"]; ok && strings.TrimSpace(rc.Name) != "" {
		return strings.TrimSpace(rc.Name)
	}
	if rc, ok := a.Config.Racks["local"]; ok && strings.TrimSpace(rc.Name) != "" {
		return strings.TrimSpace(rc.Name)
	}
	return "default"
}

func (a *App) initProxyHandler(auditLogger *audit.Logger, rackName, rackAlias string) {
	pinnedMgr := a.RackCertManager
	if !a.Config.RackTLSPinningEnabled {
		pinnedMgr = nil
	}

	a.ProxyHandler = proxy.NewHandler(
		a.Config,
		a.RBACManager,
		auditLogger,
		a.Database,
		a.SettingsService,
		a.EmailSender,
		rackName,
		rackAlias,
		pinnedMgr,
		a.MFAService,
		a.SessionManager,
	)
	a.DefaultRack = rackAlias
}

// setupRouter configures the Gin router with all routes and middleware
func (a *App) setupRouter() error {
	// Always use release mode to suppress route list printing
	gin.SetMode(gin.ReleaseMode)

	// Create router without default middleware (we'll add our own)
	router := gin.New()
	if err := router.SetTrustedProxies(a.Config.TrustedProxies); err != nil {
		return fmt.Errorf("failed to configure trusted proxies: %w", err)
	}
	// Only X-Forwarded-For, and only from TRUSTED_PROXY_CIDRS (see middleware.ClientIP).
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	if len(a.Config.TrustedProxies) == 0 && !a.Config.DevMode {
		log.Printf("TRUSTED_PROXY_CIDRS is not set: client IPs are the TCP peer. " +
			"Set it if the gateway runs behind a proxy, or audit logs and rate limits will only see the proxy.")
	}

	// Set up routes with all dependencies
	routes.Setup(router, &routes.Config{
		Gateway: a.Gateway,
		App:     a,
	})

	a.router = router
	return nil
}
