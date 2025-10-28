package deps

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/proxy"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// Gateway bundles the shared dependencies required by the application and route setup.
type Gateway struct {
	Config           *config.Config
	Database         *db.Database
	RBACManager      rbac.RBACManager
	SessionManager   *auth.SessionManager
	OAuthHandler     *auth.OAuthHandler
	AuthService      *auth.AuthService
	TokenService     *token.Service
	MFAService       *mfa.Service
	MFASettings      *db.MFASettings
	SettingsService  *settings.Service
	EmailSender      email.Sender
	ProxyHandler     *proxy.Handler
	RackCertManager  *rackcert.Manager
	SentryEnabled    bool
	AuditLogger      *audit.Logger
	DefaultRack      string
	SecurityNotifier *security.Notifier
}
