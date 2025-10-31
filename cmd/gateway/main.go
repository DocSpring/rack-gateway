package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/app"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// @title Rack Gateway API
// @version 1.0
// @description API for the Rack Gateway administration and proxy services.
// @BasePath /api/v1
// @schemes http https
// @securityDefinitions.apiKey SessionCookie
// @in header
// @name Cookie
// @description HttpOnly session cookie issued after OAuth login.
// @securityDefinitions.apiKey CSRFToken
// @in header
// @name X-CSRF-Token
// @description HMAC-derived CSRF token tied to the active session.
func main() {
	handled, err := handleMaintenanceCommand(os.Args)
	if err != nil {
		log.Fatalf("Maintenance command failed: %v", err)
	}
	if handled {
		return
	}

	if err := runGatewayServer(); err != nil {
		log.Fatalf("Gateway server error: %v", err)
	}
}

func handleMaintenanceCommand(args []string) (bool, error) {
	if len(args) <= 1 {
		return false, nil
	}

	switch args[1] {
	case "migrate":
		return true, runMigrations()
	case "reset-db":
		return true, resetDatabase()
	case "help", "--help", "-h":
		helpText := "rack-gateway commands:\n" +
			"  (no args)            Start the API server\n" +
			"  migrate             Apply database migrations\n" +
			"  reset-db            Drop and recreate the database (requires env guards)"
		fmt.Println(helpText)
		return true, nil
	default:
		return false, nil
	}
}

func runMigrations() error {
	// Always run migrations when explicitly invoked via "migrate" command
	// regardless of DEV_MODE (for production deployments)
	var dsn string
	if dsn = os.Getenv("RGW_DATABASE_URL"); dsn == "" {
		if dsn = os.Getenv("GATEWAY_DATABASE_URL"); dsn == "" {
			dsn = os.Getenv("DATABASE_URL")
		}
	}
	if dsn == "" {
		return fmt.Errorf("RGW_DATABASE_URL, GATEWAY_DATABASE_URL, or DATABASE_URL is required")
	}

	database, err := db.NewWithPoolConfigAndMigration(dsn, nil, true)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDatabase(database)

	fmt.Println("Database migrations applied")
	return nil
}

func resetDatabase() error {
	database, err := db.NewFromEnv()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDatabase(database)

	if err := database.ResetDatabase(); err != nil {
		return fmt.Errorf("reset database: %w", err)
	}
	fmt.Println("Database reset complete")
	return nil
}

func closeDatabase(database *db.Database) {
	if database == nil {
		return
	}
	if err := database.Close(); err != nil {
		log.Printf("Warning: failed to close database: %v", err)
	}
}

func runGatewayServer() error {
	application, err := app.New()
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer application.Cleanup()

	srv := &http.Server{
		Addr:    ":" + application.Config.Port,
		Handler: application.Router(),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Starting server on port %s", application.Config.Port)
		log.Printf("Visit the web UI at http://localhost:%s/", application.Config.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("server failed: %w", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	var shutdownErr error
	select {
	case sig := <-quit:
		log.Printf("Shutting down server (%s)...", sig)
	case err := <-errCh:
		shutdownErr = err
	}

	if shutdownErr != nil {
		return shutdownErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	sentry.Flush(5 * time.Second)
	log.Println("Server exited")
	return nil
}
