package app

import (
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
)

// App holds all application dependencies
type App struct {
	Config          *config.Config
	Database        *db.Database
	RBACManager     rbac.RBACManager
	JWTManager      *auth.JWTManager
	OAuthHandler    *auth.OAuthHandler
	AuthService     *auth.AuthService
	TokenService    *token.Service
	EmailSender     email.Sender
	ProxyHandler    *proxy.Handler
	RackCertManager *rackcert.Manager
	router          *gin.Engine
}

// New creates a new application instance with all dependencies initialized
func New() (*App, error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Initialize database
	database, err := db.NewFromEnv()
	if err != nil {
		return nil, err
	}

	// Initialize dependencies
	app := &App{
		Config:   cfg,
		Database: database,
	}

	// Initialize services
	if err := app.initializeServices(); err != nil {
		database.Close()
		return nil, err
	}

	// Set up router
	app.setupRouter()

	return app, nil
}

// Router returns the Gin router
func (a *App) Router() *gin.Engine {
	return a.router
}

// Cleanup cleans up resources
func (a *App) Cleanup() {
	if a.Database != nil {
		a.Database.Close()
	}
}
