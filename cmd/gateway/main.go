package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/DocSpring/convox-gateway/internal/gateway/ui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	dbPath := getEnv("CONVOX_GATEWAY_DB_PATH", "/app/data/db.sqlite")
	if cfg.DevMode && dbPath == "/app/data/db.sqlite" {
		// In dev mode, use local path if not explicitly set
		dbPath = filepath.Join(".", "data", "db.sqlite")
	}

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Initialize admin user if needed
	if len(cfg.AdminUsers) > 0 {
		adminEmail := cfg.AdminUsers[0]
		if err := database.InitializeAdmin(adminEmail, "Admin User"); err != nil {
			log.Printf("Warning: Failed to initialize admin: %v", err)
		}
	}

	// Migration from config.yml removed - database is the only source now

	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTExpiry)

	// Use database-backed RBAC manager
	allowedDomain := cfg.GoogleAllowedDomain
	rbacManager, err := rbac.NewDBManager(database, allowedDomain)
	if err != nil {
		log.Fatalf("Failed to initialize RBAC: %v", err)
	}

	// Create token service
	tokenService := token.NewService(database)

	// Create combined auth service
	authService := auth.NewAuthService(jwtManager, tokenService, database)

	// Debug: Log OAuth configuration and PORT values
	log.Printf("DEBUG: Environment PORT=%s, Config Port=%s", os.Getenv("PORT"), cfg.Port)
	log.Printf("DEBUG: OAuth config - ClientID: %s, BaseURL: %s, RedirectURL: %s",
		cfg.GoogleClientID, cfg.GoogleOAuthBaseURL, cfg.RedirectURL)

	// For OIDC, we need the issuer URL which is the base OAuth URL
	issuerURL := cfg.GoogleOAuthBaseURL
	if issuerURL == "" {
		issuerURL = "https://accounts.google.com"
	}

	oauthHandler, err := auth.NewOAuthHandler(
		cfg.GoogleClientID,
		cfg.GoogleClientSecret,
		cfg.RedirectURL,
		allowedDomain,
		issuerURL,
		jwtManager,
	)
	if err != nil {
		log.Fatalf("Failed to initialize OAuth handler: %v", err)
	}

	auditLogger := audit.NewLogger(database)
	proxyHandler := proxy.NewHandler(cfg, rbacManager, auditLogger)
	uiHandler := ui.NewHandler(rbacManager, "", tokenService, database)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Gateway's own endpoints under /.gateway/
	r.Route("/.gateway", func(r chi.Router) {
		// Health check (no auth required)
		r.Get("/health", uiHandler.Health)

		// OAuth login endpoints (no auth required)
		// CLI OAuth flow
		// Backwards-compat endpoint used by integration tests
		r.Post("/login/start", handleCLILoginStart(oauthHandler))
		r.Post("/cli/login/start", handleCLILoginStart(oauthHandler))
		r.Post("/cli/login/callback", handleCLILoginCallback(oauthHandler))

		// Web OAuth flow
		r.Get("/web/login", handleWebLoginStart(oauthHandler, database))
		r.Get("/web/callback", handleWebLoginCallback(oauthHandler, database))
		r.Get("/web/logout", handleWebLogout(database))

		// Authenticated gateway endpoints
		r.Group(func(r chi.Router) {
			r.Use(authService.Middleware)

			r.Get("/me", uiHandler.GetMe)

			// Admin endpoints
			r.Route("/admin", func(r chi.Router) {
				r.Get("/config", uiHandler.GetConfig)
				r.Put("/config", uiHandler.UpdateConfig)
				r.Get("/roles", uiHandler.ListRoles)

				// Audit log endpoints (admin)
				r.Get("/audit", uiHandler.ListAuditLogs)
				r.Get("/audit/export", uiHandler.ExportAuditLogs)

				// User management endpoints
				r.Get("/users", uiHandler.ListUsers)
				r.Post("/users", uiHandler.CreateUser)
				r.Delete("/users/{email}", uiHandler.DeleteUser)
				r.Put("/users/{email}/roles", uiHandler.UpdateUserRoles)

				// API token endpoints
				r.Post("/tokens", uiHandler.CreateAPIToken)
				r.Get("/tokens", uiHandler.ListAPITokens)
				r.Delete("/tokens/{tokenID}", uiHandler.DeleteAPIToken)
			})
		})

		// Serve UI static files
		r.Get("/ui/*", uiHandler.ServeStatic)
	})

	// Catch-all: proxy everything else to the Convox rack
	// This MUST come last to avoid catching gateway routes
	r.Group(func(r chi.Router) {
		r.Use(authService.Middleware)
		r.HandleFunc("/*", proxyHandler.ProxyToRack)
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("Starting server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func handleCLILoginStart(oauth *auth.OAuthHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := oauth.StartLogin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleWebLoginStart(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if database != nil {
			_ = database.CreateAuditLog(&db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "login.start",
				Resource:       "web",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
		}
		authURL := oauth.StartWebLogin()
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	}
}

func handleCLILoginCallback(oauth *auth.OAuthHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code         string `json:"code"`
			State        string `json:"state"`
			CodeVerifier string `json:"code_verifier"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Code == "" || req.State == "" || req.CodeVerifier == "" {
			http.Error(w, "missing required parameters", http.StatusBadRequest)
			return
		}

		resp, err := oauth.CompleteLogin(req.Code, req.State, req.CodeVerifier)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleWebLoginCallback(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" || state == "" {
			http.Error(w, "missing code or state parameter", http.StatusBadRequest)
			return
		}

		// Web flow doesn't use PKCE
		resp, err := oauth.CompleteLogin(code, state, "")
		if err != nil {
			if database != nil {
				_ = database.CreateAuditLog(&db.AuditLog{
					UserEmail:      "",
					UserName:       "",
					ActionType:     "auth",
					Action:         "login",
					Resource:       "web",
					Details:        "{\"error\":\"oauth_failed\"}",
					IPAddress:      r.RemoteAddr,
					UserAgent:      r.UserAgent(),
					Status:         "error",
					ResponseTimeMs: 0,
				})
			}
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Set auth cookie for the frontend (SameSite=Lax allows top-level redirects)
		cookie := &http.Cookie{
			Name:     "gateway_token",
			Value:    resp.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false,
			// MaxAge based on expiry (seconds)
			Expires:  resp.ExpiresAt,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, cookie)

		if database != nil {
			_ = database.CreateAuditLog(&db.AuditLog{
				UserEmail:      resp.Email,
				UserName:       resp.Name,
				ActionType:     "auth",
				Action:         "login",
				Resource:       "web",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
		}

		// Redirect back to frontend base (dev server), or root if not set
		frontend := os.Getenv("FRONTEND_BASE_URL")
		if frontend == "" {
			frontend = "/"
		}
		http.Redirect(w, r, frontend, http.StatusTemporaryRedirect)
	}
}

func handleWebLogout(database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Expire the auth cookie
		expired := &http.Cookie{
			Name:     "gateway_token",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   false,
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, expired)

		if database != nil {
			_ = database.CreateAuditLog(&db.AuditLog{
				UserEmail:      r.Header.Get("X-User-Email"),
				UserName:       r.Header.Get("X-User-Name"),
				ActionType:     "auth",
				Action:         "logout",
				Resource:       "web",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
		}

		// Redirect back to login or frontend base
		frontend := os.Getenv("FRONTEND_BASE_URL")
		if frontend == "" {
			frontend = "/login"
		} else if !strings.HasSuffix(frontend, "/login") {
			// Best-effort send to login
			frontend = frontend + "/login"
		}
		http.Redirect(w, r, frontend, http.StatusTemporaryRedirect)
	}
}
