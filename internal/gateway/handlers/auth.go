package handlers

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

// OAuthProvider captures the behavior needed from the OAuth handler.
type OAuthProvider interface {
	StartLogin() (*auth.LoginStartResponse, error)
	StartWebLogin() (authURL string, state string)
	CompleteLogin(code, state, codeVerifier string) (*auth.LoginResponse, error)
}

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	oauth            OAuthProvider
	database         *db.Database
	config           *config.Config
	sessions         *auth.SessionManager
	mfaService       *mfa.Service
	mfaSettings      *db.MFASettings
	securityNotifier *security.Notifier
	auditLogger      *audit.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(oauth OAuthProvider, database *db.Database, cfg *config.Config, sessions *auth.SessionManager, mfaService *mfa.Service, mfaSettings *db.MFASettings, securityNotifier *security.Notifier, auditLogger *audit.Logger) *AuthHandler {
	return &AuthHandler{
		oauth:            oauth,
		database:         database,
		config:           cfg,
		sessions:         sessions,
		mfaService:       mfaService,
		mfaSettings:      mfaSettings,
		securityNotifier: securityNotifier,
		auditLogger:      auditLogger,
	}
}
