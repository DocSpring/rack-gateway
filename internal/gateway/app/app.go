package app

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
)

// App holds all application dependencies
type App struct {
	*deps.Gateway
	router *gin.Engine
}

// New creates a new application instance with all dependencies initialized
func New() (*App, error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Initialize observability before creating other resources so panics during init are captured
	sentryEnabled, err := initializeSentry(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize database
	database, err := db.NewFromEnv()
	if err != nil {
		return nil, err
	}
	if err := database.EnsureEnvironment(cfg.DevMode); err != nil {
		database.Close() //nolint:errcheck // cleanup on init failure
		return nil, err
	}

	// Initialize dependencies
	app := &App{
		Gateway: &deps.Gateway{
			Config:        cfg,
			Database:      database,
			SentryEnabled: sentryEnabled,
		},
	}

	// Initialize services
	if err := app.initializeServices(); err != nil {
		database.Close() //nolint:errcheck // cleanup on init failure
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
	if a.JobsClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		a.JobsClient.Stop(ctx) //nolint:errcheck // application shutdown
	}
	if a.Database != nil {
		a.Database.Close() //nolint:errcheck // application shutdown
	}
}
