package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/routes"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
)

// initializeServices sets up all application services (matching original main.go exactly)
func (a *App) initializeServices() error {
	// Initialize admin users if configured
	if len(a.Config.AdminUsers) > 0 {
		for _, raw := range a.Config.AdminUsers {
			email := strings.TrimSpace(raw)
			if email == "" {
				continue
			}
			if err := a.Database.InitializeAdmin(email, "Admin User"); err != nil {
				log.Printf("Warning: Failed to initialize admin %s: %v", email, err)
			}
			break
		}
	}

	// Initialize JWT manager
	a.JWTManager = auth.NewJWTManager(a.Config.JWTSecret, a.Config.JWTExpiry)

	// Initialize RBAC manager
	allowedDomain := a.Config.GoogleAllowedDomain
	rbacManager, err := rbac.NewDBManager(a.Database, allowedDomain)
	if err != nil {
		return fmt.Errorf("failed to initialize RBAC: %w", err)
	}
	a.RBACManager = rbacManager

	// Seed users from environment (matching original)
	seedUsers := func(role string, emails []string, defaultName string) {
		for _, e := range emails {
			email := strings.TrimSpace(e)
			if email == "" {
				continue
			}
			uc := &rbac.UserConfig{Name: defaultName, Roles: []string{role}}
			if err := rbacManager.SaveUser(email, uc); err != nil {
				log.Printf("Warning: failed to seed %s user %s: %v", role, email, err)
			}
		}
	}

	if len(a.Config.AdminUsers) > 0 {
		seedUsers("admin", a.Config.AdminUsers, "Admin User")
	}
	if len(a.Config.ViewerUsers) > 0 {
		seedUsers("viewer", a.Config.ViewerUsers, "Viewer User")
	}
	if len(a.Config.DeployerUsers) > 0 {
		seedUsers("deployer", a.Config.DeployerUsers, "Deployer User")
	}
	if len(a.Config.OperationsUsers) > 0 {
		seedUsers("ops", a.Config.OperationsUsers, "Ops User")
	}

	// Initialize token service
	a.TokenService = token.NewService(a.Database)

	// Create combined auth service
	a.AuthService = auth.NewAuthService(a.JWTManager, a.TokenService, a.Database)

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

	// Rack TLS certificate manager
	if a.Config.RackTLSPinningEnabled {
		a.RackCertManager = rackcert.NewManager(a.Config, a.Database)
		if _, err := a.RackCertManager.TLSConfig(context.Background()); err != nil {
			log.Printf("Warning: failed to initialize rack TLS certificate: %v", err)
		}
	}

	// Seed protected env vars from DB_SEED_PROTECTED_ENV_VARS if provided
	if seed := strings.TrimSpace(os.Getenv("DB_SEED_PROTECTED_ENV_VARS")); seed != "" {
		if raw, ok, _ := a.Database.GetSettingRaw("protected_env_vars"); !ok || len(raw) == 0 {
			keys := []string{}
			for _, k := range strings.Split(seed, ",") {
				k = strings.TrimSpace(k)
				if k != "" {
					keys = append(keys, k)
				}
			}
			if len(keys) > 0 {
				_ = a.Database.UpsertSetting("protected_env_vars", keys, nil)
			}
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

	// Set up routes with all dependencies
	routes.Setup(router, &routes.Config{
		Config:       a.Config,
		Database:     a.Database,
		RBACManager:  a.RBACManager,
		JWTManager:   a.JWTManager,
		OAuthHandler: a.OAuthHandler,
		AuthService:  a.AuthService,
		TokenService: a.TokenService,
		EmailSender:  a.EmailSender,
		ProxyHandler: a.ProxyHandler,
		RackCertMgr:  a.RackCertManager,
	})

	a.router = router
}
