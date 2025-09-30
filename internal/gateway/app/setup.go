package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/routes"
	"github.com/DocSpring/convox-gateway/internal/gateway/security"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
)

// initializeServices sets up all application services (matching original main.go exactly)
func (a *App) initializeServices() error {
	if err := a.Database.SeedDatabase(&db.SeedConfig{
		AdminUsers:      a.Config.AdminUsers,
		ViewerUsers:     a.Config.ViewerUsers,
		DeployerUsers:   a.Config.DeployerUsers,
		OperationsUsers: a.Config.OperationsUsers,
	}); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	// Initialize JWT manager
	a.JWTManager = auth.NewJWTManager(a.Config.JWTSecret, a.Config.JWTExpiry)
	// Session manager enforces short-lived idle sessions for the web UI
	a.SessionManager = auth.NewSessionManager(a.Database, a.Config.JWTSecret, a.Config.SessionIdleTimeout)

	// Load MFA settings and initialize the MFA service for enrollment/verification flows
	mfaSettings, err := a.Database.GetMFASettings()
	if err != nil {
		return fmt.Errorf("failed to load MFA settings: %w", err)
	}
	if envVal := strings.TrimSpace(os.Getenv("MFA_REQUIRE_ALL_USERS")); envVal != "" {
		if parsed, err := strconv.ParseBool(envVal); err == nil && mfaSettings.RequireAllUsers != parsed {
			mfaSettings.RequireAllUsers = parsed
			if err := a.Database.UpsertMFASettings(mfaSettings, nil); err != nil {
				return fmt.Errorf("failed to apply MFA_REQUIRE_ALL_USERS override: %w", err)
			}
		}
	}
	a.MFASettings = mfaSettings

	issuerBase := strings.TrimSpace(a.Config.Domain)
	if issuerBase == "" {
		issuerBase = "Convox Gateway"
	}
	enforcedRackAlias := strings.TrimSpace(os.Getenv("RACK_ALIAS"))
	if enforcedRackAlias == "" {
		enforcedRackAlias = strings.TrimSpace(os.Getenv("RACK"))
	}
	issuer := issuerBase
	if enforcedRackAlias != "" {
		issuer = fmt.Sprintf("%s (%s)", issuerBase, enforcedRackAlias)
	}
	trustedDeviceTTL := time.Duration(mfaSettings.TrustedDeviceTTLDays) * 24 * time.Hour
	stepUpWindow := time.Duration(mfaSettings.StepUpWindowMinutes) * time.Minute
	mfaService, err := mfa.NewService(a.Database, issuer, trustedDeviceTTL, stepUpWindow, []byte(a.Config.JWTSecret))
	if err != nil {
		return fmt.Errorf("failed to initialize MFA service: %w", err)
	}
	a.MFAService = mfaService

	// Initialize RBAC manager
	allowedDomain := a.Config.GoogleAllowedDomain
	rbacManager, err := rbac.NewDBManager(a.Database, allowedDomain)
	if err != nil {
		return fmt.Errorf("failed to initialize RBAC: %w", err)
	}
	a.RBACManager = rbacManager

	// Initialize token service
	a.TokenService = token.NewService(a.Database)

	// Create combined auth service
	a.AuthService = auth.NewAuthService(a.JWTManager, a.TokenService, a.Database, a.SessionManager)

	// Debug: Log OAuth configuration (matching original)
	log.Printf("Environment PORT=%s, Config Port=%s", os.Getenv("PORT"), a.Config.Port)
	log.Printf("OAuth config - ClientID: %s, BaseURL: %s", a.Config.GoogleClientID, a.Config.GoogleOAuthBaseURL)

	// For OIDC, we need the issuer URL which is the base OAuth URL
	issuerURL := a.Config.GoogleOAuthBaseURL
	if issuerURL == "" {
		issuerURL = "https://accounts.google.com"
	}

	// Derive redirect base from DOMAIN (production) or localhost in dev
	redirectInput := ""
	if a.Config.Domain != "" {
		if strings.EqualFold(a.Config.Domain, "localhost") {
			redirectInput = "http://localhost:" + a.Config.Port
		} else {
			redirectInput = "https://" + a.Config.Domain
		}
	} else if a.Config.DevMode {
		redirectInput = "http://localhost:" + a.Config.Port
	}
	if redirectInput == "" {
		return fmt.Errorf("DOMAIN must be set (or use DEV_MODE with PORT) to derive OAuth redirect URLs")
	}

	// Initialize OAuth handler
	oauthHandler, err := auth.NewOAuthHandler(
		a.Config.GoogleClientID,
		a.Config.GoogleClientSecret,
		redirectInput,
		allowedDomain,
		issuerURL,
		a.JWTManager,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize OAuth handler: %w", err)
	}
	a.OAuthHandler = oauthHandler

	// Initialize audit logger
	auditLogger := audit.NewLogger(a.Database)
	a.AuditLogger = auditLogger

	// Rack TLS certificate manager
	if a.Config.RackTLSPinningEnabled {
		a.RackCertManager = rackcert.NewManager(a.Config, a.Database)
		if _, err := a.RackCertManager.TLSConfig(context.Background()); err != nil {
			log.Printf("Warning: failed to initialize rack TLS certificate: %v", err)
		}
	}

	// Email sender (Postmark)
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
	a.EmailSender = email.NewSender(pmToken, from, pmStream)

	// Collect admin emails for security notifications
	adminEmails := []string{}
	if allUsers, err := a.Database.ListUsers(); err == nil {
		for _, user := range allUsers {
			// Check if user has admin role
			hasAdminRole := false
			for _, role := range user.Roles {
				if role == "admin" {
					hasAdminRole = true
					break
				}
			}
			if hasAdminRole && strings.TrimSpace(user.Email) != "" {
				adminEmails = append(adminEmails, user.Email)
			}
		}
	}

	// Initialize security notifier
	a.SecurityNotifier = security.NewNotifier(a.EmailSender, auditLogger, a.Database, adminEmails)

	// Determine rack name for notifications
	rackName := strings.TrimSpace(os.Getenv("RACK"))
	if rackName == "" {
		rackName = "default"
		if rc, ok := a.Config.Racks["default"]; ok {
			if strings.TrimSpace(rc.Name) != "" {
				rackName = rc.Name
			}
		} else if rc, ok := a.Config.Racks["local"]; ok {
			if strings.TrimSpace(rc.Name) != "" {
				rackName = rc.Name
			}
		}
	}

	rackAlias := strings.TrimSpace(os.Getenv("RACK_ALIAS"))
	if rackAlias == "" {
		rackAlias = rackName
	}

	// Initialize proxy handler
	pinnedMgr := a.RackCertManager
	if !a.Config.RackTLSPinningEnabled {
		pinnedMgr = nil
	}

	a.ProxyHandler = proxy.NewHandler(a.Config, a.RBACManager, auditLogger, a.Database, a.EmailSender, rackName, rackAlias, pinnedMgr)
	a.DefaultRack = rackAlias

	return nil
}

// setupRouter configures the Gin router with all routes and middleware
func (a *App) setupRouter() {
	// Set Gin mode based on environment
	if a.Config.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create router without default middleware (we'll add our own)
	router := gin.New()
	if err := router.SetTrustedProxies(a.Config.TrustedProxies); err != nil {
		log.Fatalf("failed to configure trusted proxies: %v", err)
	}

	// Set up routes with all dependencies
	routes.Setup(router, &routes.Config{
		Config:           a.Config,
		Database:         a.Database,
		RBACManager:      a.RBACManager,
		JWTManager:       a.JWTManager,
		SessionManager:   a.SessionManager,
		OAuthHandler:     a.OAuthHandler,
		AuthService:      a.AuthService,
		TokenService:     a.TokenService,
		MFAService:       a.MFAService,
		MFASettings:      a.MFASettings,
		EmailSender:      a.EmailSender,
		ProxyHandler:     a.ProxyHandler,
		RackCertMgr:      a.RackCertManager,
		SentryEnabled:    a.SentryEnabled,
		AuditLogger:      a.AuditLogger,
		DefaultRack:      a.DefaultRack,
		SecurityNotifier: a.SecurityNotifier,
	})

	a.router = router
}
