package app

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
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
	if err := prepareDatabase(database, cfg.DevMode, os.Getenv); err != nil {
		return nil, errors.Join(err, database.Close())
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
		return nil, errors.Join(err, database.Close())
	}

	if err := app.setupRouter(); err != nil {
		app.Cleanup()
		return nil, err
	}

	return app, nil
}

// prepareDatabase checks the database is safe to use (checkProductionSafety), then in dev mode migrates it, and marks
// a new database with its environment. The check comes first: a dev build pointed at a production database must
// refuse it before changing its schema. A database without a marker is checked as the environment it would be marked.
func prepareDatabase(database *db.Database, devMode bool, getenv func(string) string) error {
	environment, err := database.CurrentEnvironment()
	if err != nil {
		return err
	}
	if environment == "" {
		environment = db.EnvironmentName(devMode)
	}
	if err := checkProductionSafety(environment, !testbypass.Compiled, getenv); err != nil {
		return err
	}
	if devMode {
		if err := database.Migrate(); err != nil {
			return err
		}
	}
	return database.EnsureEnvironment(devMode)
}

// Router returns the Gin router
func (a *App) Router() *gin.Engine {
	return a.router
}

// Cleanup cleans up resources
func (a *App) Cleanup() {
	// Repeated denials still being counted are written while the database (and the Slack job queue) are up.
	if a.AuditLogger != nil {
		a.AuditLogger.FlushDenials()
	}
	if a.JobsClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.JobsClient.Stop(ctx); err != nil {
			gtwlog.Errorf("app: stop background jobs: %v", err)
		}
	}
	if a.Database != nil {
		if err := a.Database.Close(); err != nil {
			gtwlog.Errorf("app: close database: %v", err)
		}
	}
}
