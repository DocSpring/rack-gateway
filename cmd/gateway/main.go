package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/logging"
	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/DocSpring/convox-gateway/internal/gateway/ratelimit"
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
		case "migrate":
			// One-off migration runner
			database, err := db.NewFromEnv()
			if err != nil {
				log.Fatalf("Failed to open database: %v", err)
			}
			defer database.Close()
			fmt.Println("Database migrations applied")
			return
		case "help", "--help", "-h":
			fmt.Println("convox-gateway commands:\n  (no args)            Start the API server")
			return
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	database, err := db.NewFromEnv()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// We do not delete audit logs on boot; keep all logs.

	// Initialize admin user if configured (only needed when database is empty)
	if len(cfg.AdminUsers) > 0 {
		for _, raw := range cfg.AdminUsers {
			email := strings.TrimSpace(raw)
			if email == "" {
				continue
			}
			if err := database.InitializeAdmin(email, "Admin User"); err != nil {
				log.Printf("Warning: Failed to initialize admin %s: %v", email, err)
			}
			break
		}
	}

	// Database is the only source of configuration/state now

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
	if len(cfg.AdminUsers) > 0 {
		seedUsers("admin", cfg.AdminUsers, "Admin User")
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

	// Debug: Log OAuth configuration and PORT values via log levels
	logging.Debugf("Environment PORT=%s, Config Port=%s", os.Getenv("PORT"), cfg.Port)
	logging.Debugf("OAuth config - ClientID: %s, BaseURL: %s", cfg.GoogleClientID, cfg.GoogleOAuthBaseURL)

	// For OIDC, we need the issuer URL which is the base OAuth URL
	issuerURL := cfg.GoogleOAuthBaseURL
	if issuerURL == "" {
		issuerURL = "https://accounts.google.com"
	}

	// Derive redirect base from DOMAIN (production) or localhost in dev
	// For localhost, always include scheme http and the bound port so callbacks hit the running gateway.
	redirectInput := ""
	if cfg.Domain != "" {
		if strings.EqualFold(cfg.Domain, "localhost") {
			redirectInput = "http://localhost:" + cfg.Port
		} else {
			redirectInput = "https://" + cfg.Domain
		}
	} else if cfg.DevMode {
		redirectInput = "http://localhost:" + cfg.Port
	}
	if redirectInput == "" {
		log.Fatalf("DOMAIN must be set (or use DEV_MODE with PORT) to derive OAuth redirect URLs")
	}
	oauthHandler, err := auth.NewOAuthHandler(
		cfg.GoogleClientID,
		cfg.GoogleClientSecret,
		redirectInput,
		allowedDomain,
		issuerURL,
		jwtManager,
	)
	if err != nil {
		log.Fatalf("Failed to initialize OAuth handler: %v", err)
	}

	auditLogger := audit.NewLogger(database)
	// Seed protected env vars from DB_SEED_PROTECTED_ENV_VARS if provided and not set
	if seed := strings.TrimSpace(os.Getenv("DB_SEED_PROTECTED_ENV_VARS")); seed != "" {
		if raw, ok, _ := database.GetSettingRaw("protected_env_vars"); !ok || len(raw) == 0 {
			keys := []string{}
			for _, k := range strings.Split(seed, ",") {
				k = strings.TrimSpace(k)
				if k != "" {
					keys = append(keys, k)
				}
			}
			if len(keys) > 0 {
				_ = database.UpsertSetting("protected_env_vars", keys, nil)
			}
		}
	}

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

	// Determine rack config/name for notifications and downstream API
	rackCfg := config.RackConfig{}
	// Prefer explicit RACK env (Convox provides this). Fallback to config racks.
	rackName := strings.TrimSpace(os.Getenv("RACK"))
	if rackName == "" {
		rackName = "default"
		if rc, ok := cfg.Racks["default"]; ok {
			rackCfg = rc
			if strings.TrimSpace(rc.Name) != "" {
				rackName = rc.Name
			}
		} else if rc, ok := cfg.Racks["local"]; ok {
			rackCfg = rc
			if strings.TrimSpace(rc.Name) != "" {
				rackName = rc.Name
			}
		}
	} else {
		// Try to populate rackCfg if there is a matching entry (optional)
		if rc, ok := cfg.Racks[rackName]; ok {
			rackCfg = rc
		} else if rc, ok := cfg.Racks["default"]; ok {
			rackCfg = rc
		}
	}

	rackAlias := strings.TrimSpace(os.Getenv("RACK_ALIAS"))
	if rackAlias == "" {
		rackAlias = rackName
	}

	// Dev proxy for web UI: if DEV_MODE and WEB_DEV_SERVER_URL provided, proxy /.gateway/web/* to Vite
	devProxyURL := ""
	if getEnv("DEV_MODE", "false") == "true" {
		devProxyURL = os.Getenv("WEB_DEV_SERVER_URL")
	}
	// Public base URL used in emails and links
	publicBase := redirectInput
	// Initialize proxy with email sender and rack name for notifications
	proxyHandler := proxy.NewHandler(cfg, rbacManager, auditLogger, database, sender, rackName, rackAlias)

	uiHandler := ui.NewHandler(rbacManager, "", tokenService, database, sender, rackName, rackAlias, rackCfg, devProxyURL, publicBase)

	// Initialize rate limiter for auth endpoints
	// 10 requests per second with burst of 20 - very generous for auth
	// This allows for normal OAuth flow and some retries without blocking legitimate users
	rateLimiter := ratelimit.NewRateLimiter(10, 20)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(filteredLogger())
	r.Use(middleware.Recoverer)

	// Auth + UI
	// Quiet common browser noise to avoid console 404s
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Get("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("User-agent: *\nDisallow:"))
	})

	r.Get("/.gateway/web", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/.gateway/web/rack", http.StatusTemporaryRedirect)
	})
	r.Get("/.gateway/web/*", uiHandler.ServeStatic)

	// Expose API only under /.gateway/api/* (apply timeout here, not on WS proxy routes)
	r.Route("/.gateway/api", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))

		// Apply rate limiting to auth endpoints
		r.Group(func(r chi.Router) {
			r.Use(rateLimiter.Middleware)

			// Auth (scoped under /auth) - rate limited
			r.Post("/auth/cli/start", handleCLILoginStart(oauthHandler, database))
			r.Get("/auth/cli/callback", handleCLILoginRedirectCallback(database))
			r.Post("/auth/cli/complete", handleCLILoginComplete(oauthHandler, database))

			// OAuth (web) endpoints - rate limited
			// Accept both GET and HEAD for login to support headless browser probes
			r.Get("/auth/web/login", handleWebLoginStart(oauthHandler, database))
			r.Head("/auth/web/login", handleWebLoginStart(oauthHandler, database))
			r.Get("/auth/web/callback", handleWebLoginCallback(oauthHandler, database))
			r.Get("/auth/web/logout", handleWebLogout(database))

			// CSRF for web under /auth/web (rate limited as it's auth-related)
			r.Get("/auth/web/csrf", uiHandler.GetCSRFToken)
		})

		// Health (no auth or rate limiting required)
		r.Get("/health", uiHandler.Health)

		// Authenticated endpoints
		r.Group(func(r chi.Router) {
			r.Use(authService.Middleware)
			r.Get("/me", uiHandler.GetMe)
			r.Get("/created-by", uiHandler.GetCreators)
			// Rack info (non-admin, read-only)
			r.Get("/rack", uiHandler.GetRackInfo)
			// Env view API (safe masking by default, request secrets via ?secrets=true)
			r.Get("/env", uiHandler.GetEnvValues)

			r.Route("/admin", func(r chi.Router) {
				r.Use(csrfMiddleware())
				r.Get("/config", uiHandler.GetConfig)
				r.Put("/config", uiHandler.UpdateConfig)
				// Settings endpoints
				r.Get("/settings", uiHandler.GetSettings)
				r.Put("/settings/protected_env_vars", uiHandler.UpdateProtectedEnvVars)
				r.Put("/settings/allow_destructive_actions", uiHandler.UpdateAllowDestructiveActions)
				// Dev-only utilities (compiled in with -tags=dev)
				ui.RegisterDevAdminRoutes(r, uiHandler)
				r.Get("/roles", uiHandler.ListRoles)
				r.Get("/audit", uiHandler.ListAuditLogs)
				r.Get("/audit/export", uiHandler.ExportAuditLogs)
				r.Get("/users", uiHandler.ListUsers)
				r.Post("/users", uiHandler.CreateUser)
				r.Delete("/users/{email}", uiHandler.DeleteUser)
				r.Put("/users/{email}", uiHandler.UpdateUserProfile)
				r.Put("/users/{email}/roles", uiHandler.UpdateUserRoles)

				// Apply rate limiting to token creation (auth-related)
				r.Group(func(r chi.Router) {
					r.Use(rateLimiter.Middleware)
					r.Post("/tokens", uiHandler.CreateAPIToken)
				})

				r.Get("/tokens", uiHandler.ListAPITokens)
				r.Get("/tokens/{tokenID}", uiHandler.GetAPIToken)
				r.Get("/tokens/permissions", uiHandler.GetTokenPermissionMetadata)
				r.Put("/tokens/{tokenID}", uiHandler.UpdateAPIToken)
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
			if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
				next.ServeHTTP(w, r)
				return
			}
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
		writeAudit := func(status string) {
			if database == nil {
				return
			}
			_ = audit.LogDB(database, &db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "login.start",
				ResourceType:   "auth",
				Resource:       "cli",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         status,
				ResponseTimeMs: 0,
			})
		}

		resp, err := oauth.StartLogin()
		if err != nil {
			writeAudit("error")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeAudit("success")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleWebLoginStart(oauth *auth.OAuthHandler, database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Optional detailed OAuth debug logging
		logging.Debugf("[oauth:web] start login req_id=%s host=%s ua=%q", middleware.GetReqID(r.Context()), r.Host, r.UserAgent())
		if database != nil {
			_ = audit.LogDB(database, &db.AuditLog{
				UserEmail:      "",
				UserName:       "",
				ActionType:     "auth",
				Action:         "login.start",
				ResourceType:   "auth",
				Resource:       "web",
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "success",
				ResponseTimeMs: 0,
			})
		}
		authURL := oauth.StartWebLogin()
		if u, err := url.Parse(authURL); err == nil {
			reqID := middleware.GetReqID(r.Context())
			logging.Debugf("[oauth:web] redirecting req_id=%s to auth_endpoint host=%s path=%s (query=redacted)", reqID, u.Host, u.Path)
		}
		// Use 302 (Found) for broad browser compatibility with navigation flows
		w.Header().Set("Allow", "GET, HEAD")
		http.Redirect(w, r, authURL, http.StatusFound)
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
		logging.Debugf("[oauth:cli] redirect callback req_id=%s code_present=%t state_present=%t", middleware.GetReqID(r.Context()), code != "", state != "")
		if database != nil {
			_ = database.SaveCLILoginCode(state, code)
		}

		// Redirect to a nicer static success page served by the web bundle
		logging.Debugf("[oauth:cli] redirecting req_id=%s to /.gateway/web/cli-auth-success.html", middleware.GetReqID(r.Context()))
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
					Details:        "error\":\"oauth_failed\"}",
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

		logging.Debugf("[oauth:web] callback req_id=%s code_present=%t state_present=%t", middleware.GetReqID(r.Context()), code != "", state != "")
		// Web flow doesn't use PKCE
		resp, err := oauth.CompleteLogin(code, state, "")
		if err != nil {
			if database != nil {
				_ = audit.LogDB(database, &db.AuditLog{
					UserEmail:      "",
					UserName:       "",
					ActionType:     "auth",
					Action:         "login",
					ResourceType:   "auth",
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
				ResourceType:   "auth",
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
		// Normalize
		if !strings.HasSuffix(frontend, "/") {
			frontend += "/"
		}
		// Compute default landing page: Users list under the web UI base
		trimmed := strings.TrimRight(frontend, "/")
		dest := frontend
		if strings.HasSuffix(trimmed, "/.gateway/web") {
			// Frontend already points at web base; append users
			dest = frontend + "users"
		} else {
			// Frontend is likely a dev root like http://localhost:5223/
			// Send to the web base path with users
			dest = strings.TrimRight(frontend, "/") + "/.gateway/web/users"
		}
		logging.Debugf("[oauth:web] redirecting req_id=%s to frontend=%s", middleware.GetReqID(r.Context()), dest)
		http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
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
				ResourceType:   "auth",
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
