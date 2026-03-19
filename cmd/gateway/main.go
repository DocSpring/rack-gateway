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

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/app"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
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

	// If we get here, no maintenance command was recognized.
	// Crash if any arguments were passed (server mode takes no arguments).
	if len(os.Args) > 1 {
		// Sanitize args to prevent log injection
		sanitized := make([]string, len(os.Args[1:]))
		for i, arg := range os.Args[1:] {
			sanitized[i] = logutil.SanitizeForLog(arg)
		}
		log.Fatalf(
			"Server mode does not accept arguments, got: %v\n\nUse 'help' to see available commands",
			sanitized,
		)
	}

	if err := runGatewayServer(); err != nil {
		log.Fatalf("Gateway server error: %v", err)
	}
}

func handleMaintenanceCommand(args []string) (bool, error) {
	if len(args) <= 1 {
		return false, nil
	}

	cmd := args[1]

	// Check if this is a valid command first
	var isValidCommand bool
	switch cmd {
	case "migrate", "reset-db", "write-anchor", "help", "--help", "-h":
		isValidCommand = true
	}

	// If it's NOT a valid command, return unknown command error immediately
	if !isValidCommand {
		return true, fmt.Errorf("unknown command: %s\n\nUse 'help' to see available commands", cmd)
	}

	// For valid commands, validate no extra arguments
	if len(args) > 2 {
		return true, fmt.Errorf("'%s' command does not accept arguments, got: %v", cmd, args[2:])
	}

	// Execute the command
	switch cmd {
	case "migrate":
		return true, runMigrations()
	case "reset-db":
		return true, resetDatabase()
	case "write-anchor":
		return true, writeAuditAnchor()
	case "help", "--help", "-h":
		helpText := "rack-gateway commands:\n" +
			"  (no args)            Start the API server\n" +
			"  migrate             Apply database migrations\n" +
			"  reset-db            Drop and recreate the database (requires env guards)\n" +
			"  write-anchor        Manually trigger an audit anchor write to S3"
		fmt.Println(helpText)
		return true, nil
	default:
		// Should never reach here due to isValidCommand check above
		return true, fmt.Errorf("unknown command: %s\n\nUse 'help' to see available commands", cmd)
	}
}

// getAdminDatabaseURL returns the database URL for admin operations (migrations, resets).
// Prefers ADMIN_DATABASE_URL if available, falls back to other connection strings.
func getAdminDatabaseURL() (string, error) {
	var dsn string
	if dsn = os.Getenv("ADMIN_DATABASE_URL"); dsn == "" {
		if dsn = os.Getenv("RGW_DATABASE_URL"); dsn == "" {
			if dsn = os.Getenv("GATEWAY_DATABASE_URL"); dsn == "" {
				dsn = os.Getenv("DATABASE_URL")
			}
		}
	}
	if dsn == "" {
		return "", fmt.Errorf("ADMIN_DATABASE_URL, RGW_DATABASE_URL, GATEWAY_DATABASE_URL, or DATABASE_URL is required")
	}
	return dsn, nil
}

func runMigrations() error {
	dsn, err := getAdminDatabaseURL()
	if err != nil {
		return err
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
	dsn, err := getAdminDatabaseURL()
	if err != nil {
		return err
	}

	database, err := db.NewWithPoolConfigAndMigration(dsn, nil, false)
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

func writeAuditAnchor() error {
	// Load audit anchor configuration from environment
	anchorConfig, err := jobs.NewAuditAnchorConfigFromEnv()
	if err != nil {
		return fmt.Errorf("failed to load audit anchor config: %w", err)
	}
	if anchorConfig == nil {
		return fmt.Errorf("audit anchor not configured - set AUDIT_ANCHOR_BUCKET and AUDIT_ANCHOR_CHAIN_ID")
	}

	// Connect to database
	database, err := db.NewFromEnv()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer closeDatabase(database)

	// Create anchor writer worker directly
	s3Client, ok := anchorConfig.S3Client.(*s3.Client)
	if !ok {
		return fmt.Errorf("invalid S3 client type")
	}

	worker := jobaudit.NewAnchorWriterWorker(
		database,
		s3Client,
		anchorConfig.Bucket,
		anchorConfig.ChainID,
		anchorConfig.RetentionDays,
	)

	// Execute the work directly (synchronously)
	ctx := context.Background()
	mockJob := &river.Job[jobaudit.AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Now(),
		},
	}

	fmt.Println("Writing audit anchor to S3...")
	if err := worker.Work(ctx, mockJob); err != nil {
		return fmt.Errorf("failed to write anchor: %w", err)
	}

	fmt.Println("✓ Audit anchor written to S3 successfully")
	return nil
}

func runGatewayServer() error {
	application, err := app.New()
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer application.Cleanup()

	srv := &http.Server{
		Addr:              ":" + application.Config.Port,
		Handler:           application.Router(),
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
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
