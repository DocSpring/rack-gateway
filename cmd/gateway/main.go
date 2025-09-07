package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/DocSpring/convox-gateway/internal/gateway/ui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Support maintenance subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "audit-cleanup":
			// Usage: convox-gateway audit-cleanup --days 90
			// Falls back to AUDIT_LOG_RETENTION_DAYS if --days not provided
			days := 0
			// Parse a very small flag set: --days N
			for i := 2; i < len(os.Args); i++ {
				if strings.HasPrefix(os.Args[i], "--days=") {
					if v, err := strconv.Atoi(strings.TrimPrefix(os.Args[i], "--days=")); err == nil {
						days = v
					}
				} else if os.Args[i] == "--days" && i+1 < len(os.Args) {
					if v, err := strconv.Atoi(os.Args[i+1]); err == nil {
						days = v
					}
					i++
				}
			}
			if days == 0 {
				if ds := os.Getenv("AUDIT_LOG_RETENTION_DAYS"); ds != "" {
					if v, err := strconv.Atoi(ds); err == nil {
						days = v
					}
				}
			}
			if days <= 0 {
				log.Fatalf("audit-cleanup requires --days N or AUDIT_LOG_RETENTION_DAYS")
			}

			dbPath := getEnv("GATEWAY_DB_PATH", "/app/data/db.sqlite")
			database, err := db.New(dbPath)
			if err != nil {
				log.Fatalf("Failed to open database: %v", err)
			}
			defer database.Close()
			if err := database.CleanupOldAuditLogs(days); err != nil {
				log.Fatalf("Audit cleanup failed: %v", err)
			}
			log.Printf("Audit cleanup successful: removed entries older than %d days", days)
			return
		case "help", "--help", "-h":
			fmt.Println("convox-gateway commands:\n  (no args)            Start the API server\n  audit-cleanup        Delete audit logs older than N days\n                        --days N (or set AUDIT_LOG_RETENTION_DAYS)")
			return
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	dbPath := getEnv("GATEWAY_DB_PATH", "/app/data/db.sqlite")

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Audit retention cleanup
	if daysStr := os.Getenv("AUDIT_LOG_RETENTION_DAYS"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			if err := database.CleanupOldAuditLogs(d); err != nil {
				log.Printf("Warning: audit retention cleanup failed: %v", err)
			}
		}
	}

	// Initialize admin user if needed (legacy path)
	if len(cfg.AdminUsers) > 0 {
		adminEmail := strings.TrimSpace(cfg.AdminUsers[0])
		if adminEmail != "" {
			if err := database.InitializeAdmin(adminEmail, "Admin User"); err != nil {
				log.Printf("Warning: Failed to initialize admin: %v", err)
			}
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

	// Seed users from environment for dev/test convenience
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
	if len(cfg.ViewerUsers) > 0 {
		seedUsers("viewer", cfg.ViewerUsers, "Viewer User")
	}
	if len(cfg.DeployerUsers) > 0 {
		seedUsers("deployer", cfg.DeployerUsers, "Deployer User")
	}
	if len(cfg.OperationsUsers) > 0 {
		seedUsers("ops", cfg.OperationsUsers, "Ops User")
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

	// Email sender (Postmark)
	pmToken := os.Getenv("POSTMARK_API_TOKEN")
	from := os.Getenv("POSTMARK_FROM")
	if from == "" {
		domain := cfg.GoogleAllowedDomain
		if domain == "" {
			domain = "localhost"
		}
		from = "no-reply@" + domain
	}
	pmStream := os.Getenv("POSTMARK_STREAM")
	sender := email.NewSender(pmToken, from, pmStream)

	// Determine rack name for notifications
	rackName := "default"
	if rc, ok := cfg.Racks["default"]; ok {
		rackName = rc.Name
	} else if rc, ok := cfg.Racks["local"]; ok {
		rackName = rc.Name
	}

	// Dev proxy for web UI: if DEV_MODE and WEB_DEV_SERVER_URL provided, proxy /.gateway/web/* to Vite
	devProxyURL := ""
	if getEnv("DEV_MODE", "false") == "true" {
		devProxyURL = os.Getenv("WEB_DEV_SERVER_URL")
	}
	uiHandler := ui.NewHandler(rbacManager, "", tokenService, database, sender, rackName, devProxyURL)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(filteredLogger())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CLI OAuth endpoints under /.gateway/api/cli/*
	// Serve UI static files under /.gateway/web/
	r.Get("/.gateway/web", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/.gateway/web/", http.StatusMovedPermanently)
	})
	r.Get("/.gateway/web/*", uiHandler.ServeStatic)

	// Expose API only under /.gateway/api/*
	r.Route("/.gateway/api", func(r chi.Router) {
		// CLI OAuth endpoints
		r.Post("/cli/login/start", handleCLILoginStart(oauthHandler, database))
		r.Get("/cli/login/callback", handleCLILoginRedirectCallback(database))
		r.Post("/cli/login/complete", handleCLILoginComplete(oauthHandler, database))
		// Health + CSRF (no auth required)
		r.Get("/health", uiHandler.Health)
		r.Get("/csrf", uiHandler.GetCSRFToken)

		// OAuth (web) endpoints
		r.Get("/web/login", handleWebLoginStart(oauthHandler, database))
		r.Get("/web/callback", handleWebLoginCallback(oauthHandler, database))
		r.Get("/web/logout", handleWebLogout(database))

		// Authenticated endpoints
		r.Group(func(r chi.Router) {
			r.Use(authService.Middleware)
			r.Get("/me", uiHandler.GetMe)

			r.Route("/admin", func(r chi.Router) {
				r.Use(csrfMiddleware())
				r.Get("/config", uiHandler.GetConfig)
				r.Put("/config", uiHandler.UpdateConfig)
				r.Get("/roles", uiHandler.ListRoles)
				r.Get("/audit", uiHandler.ListAuditLogs)
				r.Get("/audit/export", uiHandler.ExportAuditLogs)
				r.Get("/users", uiHandler.ListUsers)
				r.Post("/users", uiHandler.CreateUser)
				r.Delete("/users/{email}", uiHandler.DeleteUser)
				r.Put("/users/{email}/roles", uiHandler.UpdateUserRoles)
				r.Post("/tokens", uiHandler.CreateAPIToken)
				r.Get("/tokens", uiHandler.ListAPITokens)
				r.Put("/tokens/{tokenID}", uiHandler.UpdateAPITokenName)
				r.Delete("/tokens/{tokenID}", uiHandler.DeleteAPIToken)
			})
		})
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

// filteredLogger suppresses noisy healthcheck logs (200 OK) while logging others.
func filteredLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			// Suppress successful health checks to reduce log noise/cost
			if r.URL.Path == "/.gateway/api/health" && ww.Status() == http.StatusOK {
				return
			}
			// Basic concise line; audit and DB logs provide detailed context elsewhere
			log.Printf("%s %s %d in %s", r.Method, r.URL.String(), ww.Status(), time.Since(start))
		})
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// csrfMiddleware validates that unsafe admin requests include a CSRF token header matching the CSRF cookie.
func csrfMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			default:
				hdr := r.Header.Get("X-CSRF-Token")
				c, err := r.Cookie("csrf_token")
				if err != nil || hdr == "" || c == nil || c.Value == "" || hdr != c.Value {
					http.Error(w, "invalid CSRF token", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			}
		})
	}
}

func handleCLILoginStart(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := oauth.StartLogin()
		if err != nil {
			if database != nil {
				_ = audit.LogDB(database, &db.AuditLog{
					UserEmail:      "",
					UserName:       "",
					ActionType:     "auth",
					Action:         "login.start",
					Resource:       "cli",
					Details:        "{}",
					IPAddress:      r.RemoteAddr,
					UserAgent:      r.UserAgent(),
					Status:         "error",
					ResponseTimeMs: 0,
				})
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if database != nil {
			_ = audit.LogDB(database, &db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "login.start",
				Resource:       "cli",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleWebLoginStart(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if database != nil {
			_ = audit.LogDB(database, &db.AuditLog{
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

// handleCLILoginRedirectCallback receives GET redirects from the OIDC provider
// and stores the authorization code by state for later completion by the CLI.
func handleCLILoginRedirectCallback(database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" || state == "" {
			http.Error(w, "missing code or state parameter", http.StatusBadRequest)
			return
		}
		if database != nil {
			_ = database.SaveCLILoginCode(state, code)
		}

		// Redirect to a nicer static success page served by the web bundle
		http.Redirect(w, r, "/.gateway/web/cli-auth-success.html", http.StatusTemporaryRedirect)
	}
}

// handleCLILoginComplete exchanges the stored code for a token using the provided state + code_verifier.
func handleCLILoginComplete(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State        string `json:"state"`
			CodeVerifier string `json:"code_verifier"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.State == "" || req.CodeVerifier == "" {
			http.Error(w, "missing required parameters", http.StatusBadRequest)
			return
		}

		code, ok, err := database.GetCLILoginCode(req.State)
		if err != nil {
			http.Error(w, "failed to retrieve login state", http.StatusInternalServerError)
			return
		}
		if !ok {
			// Not ready yet
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"pending"}`))
			return
		}

		resp, err := oauth.CompleteLogin(code, req.State, req.CodeVerifier)
		if err != nil {
			if database != nil {
				_ = database.CreateAuditLog(&db.AuditLog{
					UserEmail:      "",
					UserName:       "",
					ActionType:     "auth",
					Action:         "login",
					Resource:       "cli",
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

		// Clear stored code
		_ = database.DeleteCLILoginCode(req.State)

		if database != nil {
			_ = database.CreateAuditLog(&db.AuditLog{
				UserEmail:      resp.Email,
				UserName:       resp.Name,
				ActionType:     "auth",
				Action:         "login",
				Resource:       "cli",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
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
				_ = audit.LogDB(database, &db.AuditLog{
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
		// Set auth cookie for the frontend
		// Use secure cookies by default; allow insecure only in explicit dev mode
		secureCookies := getEnv("COOKIE_SECURE", "true") != "false"
		if getEnv("DEV_MODE", "false") == "true" {
			secureCookies = false
		}
		cookie := &http.Cookie{
			Name:     "gateway_token",
			Value:    resp.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookies,
			// MaxAge based on expiry (seconds)
			Expires:  resp.ExpiresAt,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, cookie)

		if database != nil {
			_ = audit.LogDB(database, &db.AuditLog{
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

		// Redirect back to frontend base (dev server) or gateway UI path
		frontend := os.Getenv("FRONTEND_BASE_URL")
		if frontend == "" {
			frontend = "/.gateway/web/"
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
			_ = audit.LogDB(database, &db.AuditLog{
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
