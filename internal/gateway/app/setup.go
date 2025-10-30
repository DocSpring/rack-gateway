package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/asyncmail"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
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
	issuer           string
	trustedDeviceTTL time.Duration
	stepUpWindow     time.Duration
	yubiClientID     string
	yubiSecretKey    string
	webAuthnRPID     string
	webAuthnOrigin   string
}

// initializeServices sets up all application services (matching original main.go exactly)
func (a *App) initializeServices() error {
	if err := a.ensureAuditSecret(); err != nil {
		return err
	}
	if err := a.seedInitialUsers(); err != nil {
		return err
	}

	a.initSessionManager()

	mfaCfg, err := a.initSettingsAndMFAConfig()
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

	deliverySender := a.newEmailSender()
	if err := a.initJobsClient(deliverySender, slackNotifier); err != nil {
		return err
	}

	if err := a.initMFAService(mfaCfg); err != nil {
		return err
	}

	adminEmails := a.collectAdminEmails()
	a.configureAuditEnqueuer(auditLogger)
	a.startJobsWorker()
	a.initSecurityNotifier(auditLogger, adminEmails)

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

func (a *App) seedInitialUsers() error {
	return a.Database.SeedDatabase(&db.SeedConfig{
		AdminUsers:      a.Config.AdminUsers,
		ViewerUsers:     a.Config.ViewerUsers,
		DeployerUsers:   a.Config.DeployerUsers,
		OperationsUsers: a.Config.OperationsUsers,
	})
}

func (a *App) initSessionManager() {
	a.SessionManager = auth.NewSessionManager(a.Database, a.Config.SessionSecret, a.Config.SessionIdleTimeout)
}

func (a *App) initSettingsAndMFAConfig() (*mfaRuntimeConfig, error) {
	a.SettingsService = settings.NewService(a.Database)

	mfaSettings, err := a.SettingsService.GetMFASettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load MFA settings: %w", err)
	}
	a.MFASettings = mfaSettings

	cfg := &mfaRuntimeConfig{
		issuer:           deriveMFAIssuer(),
		trustedDeviceTTL: time.Duration(mfaSettings.TrustedDeviceTTLDays) * 24 * time.Hour,
		stepUpWindow:     time.Duration(mfaSettings.StepUpWindowMinutes) * time.Minute,
		yubiClientID:     strings.TrimSpace(os.Getenv("YUBICO_CLIENT_ID")),
		yubiSecretKey:    strings.TrimSpace(os.Getenv("YUBICO_SECRET_KEY")),
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

	if a.Config.DevMode && (a.Config.Domain == "localhost" || strings.HasPrefix(a.Config.Domain, "localhost:")) {
		return rpid, fmt.Sprintf("http://localhost:%s", a.Config.Port)
	}
	if a.Config.Domain == "" {
		return rpid, origin
	}

	scheme := "https"
	if a.Config.Domain == "localhost" || strings.HasPrefix(a.Config.Domain, "localhost:") {
		scheme = "http"
	}
	return rpid, fmt.Sprintf("%s://%s", scheme, a.Config.Domain)
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

	log.Printf("Environment PORT=%s, Config Port=%s", os.Getenv("PORT"), a.Config.Port)
	log.Printf("OAuth config - ClientID: %s, BaseURL: %s", a.Config.GoogleClientID, a.Config.GoogleOAuthBaseURL)
}

func (a *App) resolveOAuthParameters() (string, string, string, error) {
	issuerURL := a.Config.GoogleOAuthBaseURL
	if issuerURL == "" {
		issuerURL = "https://accounts.google.com"
	}

	redirectInput := ""
	switch {
	case a.Config.Domain != "" && strings.EqualFold(a.Config.Domain, "localhost"):
		redirectInput = "http://localhost:" + a.Config.Port
	case a.Config.Domain != "":
		redirectInput = "https://" + a.Config.Domain
	case a.Config.DevMode:
		redirectInput = "http://localhost:" + a.Config.Port
	}

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
	notifier := slackpkg.NewNotifier(a.Database)
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

func (a *App) initJobsClient(sender email.Sender, notifier *slackpkg.Notifier) error {
	client, err := jobs.NewClient(a.Database.Pool(), &jobs.Dependencies{
		Database:      a.Database,
		EmailSender:   sender,
		SlackNotifier: notifier,
	})
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
		cfg.trustedDeviceTTL,
		cfg.stepUpWindow,
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

func (a *App) collectAdminEmails() []string {
	users, err := a.Database.ListUsers()
	if err != nil {
		return nil
	}
	var emails []string
	for _, user := range users {
		if strings.TrimSpace(user.Email) == "" {
			continue
		}
		for _, role := range user.Roles {
			if role == "admin" {
				emails = append(emails, user.Email)
				break
			}
		}
	}
	return emails
}

func (a *App) configureAuditEnqueuer(logger *audit.Logger) {
	logger.SetAuditEventEnqueuer(jobs.NewAuditEventEnqueuer(a.JobsClient))
}

func (a *App) startJobsWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	a.WorkerCtx = ctx
	a.WorkerCancel = cancel
	a.WorkerWg.Add(1)
	go func() {
		defer a.WorkerWg.Done()
		if err := a.JobsClient.Start(ctx); err != nil {
			log.Printf("ERROR: Failed to start jobs worker: %v", err)
		}
	}()
}

func (a *App) initSecurityNotifier(logger *audit.Logger, adminEmails []string) {
	a.SecurityNotifier = security.NewNotifier(a.EmailSender, logger, a.Database, adminEmails, a.JobsClient)
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

	a.ProxyHandler = proxy.NewHandler(a.Config, a.RBACManager, auditLogger, a.Database, a.SettingsService, a.EmailSender, rackName, rackAlias, pinnedMgr, a.MFAService, a.SessionManager)
	a.DefaultRack = rackAlias
}

// setupRouter configures the Gin router with all routes and middleware
func (a *App) setupRouter() {
	// Always use release mode to suppress route list printing
	gin.SetMode(gin.ReleaseMode)

	// Create router without default middleware (we'll add our own)
	router := gin.New()
	if err := router.SetTrustedProxies(a.Config.TrustedProxies); err != nil {
		log.Fatalf("failed to configure trusted proxies: %v", err)
	}

	// Set up routes with all dependencies
	routes.Setup(router, &routes.Config{
		Gateway: a.Gateway,
		App:     a,
	})

	a.router = router
}
