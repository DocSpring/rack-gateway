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

	"github.com/DocSpring/convox-gateway/internal/gateway/app"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
)

// @title Convox Gateway API
// @version 1.0
// @description API for the Convox Gateway administration and proxy services.
// @BasePath /.gateway/api
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
	// Support maintenance subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			database, err := db.NewFromEnv()
			if err != nil {
				log.Fatalf("Failed to open database: %v", err)
			}
			defer database.Close()
			fmt.Println("Database migrations applied")
			return
		case "reset-db":
			database, err := db.NewFromEnv()
			if err != nil {
				log.Fatalf("Failed to open database: %v", err)
			}
			defer database.Close()
			if err := database.ResetDatabase(); err != nil {
				log.Fatalf("Database reset failed: %v", err)
			}
			fmt.Println("Database reset complete")
			return
		case "help", "--help", "-h":
			fmt.Println("convox-gateway commands:\n  (no args)            Start the API server\n  migrate             Apply database migrations\n  reset-db            Drop and recreate the database (requires env guards)")
			return
		}
	}

	// Initialize and run the application
	application, err := app.New()
	if err != nil {
		log.Fatalf("Failed to initialize app: %v", err)
	}
	defer application.Cleanup()

	// Get the router
	router := application.Router()

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + application.Config.Port,
		Handler: router,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting server on port %s", application.Config.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
