package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docspring/convox-gateway/internal/api/audit"
	"github.com/docspring/convox-gateway/internal/api/auth"
	"github.com/docspring/convox-gateway/internal/api/config"
	"github.com/docspring/convox-gateway/internal/api/proxy"
	"github.com/docspring/convox-gateway/internal/api/rbac"
	"github.com/docspring/convox-gateway/internal/api/ui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("No .env file found: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTExpiry)
	
	rbacManager, err := rbac.NewManager(cfg.ConfigPath)
	if err != nil {
		log.Fatalf("Failed to initialize RBAC: %v", err)
	}
	
	// Use domain from config.yml if available, otherwise fall back to env var
	allowedDomain := rbacManager.GetDomain()
	if allowedDomain == "" {
		allowedDomain = cfg.GoogleAllowedDomain
	}
	
	oauthHandler := auth.NewOAuthHandler(
		cfg.GoogleClientID,
		cfg.GoogleClientSecret,
		cfg.RedirectURL,
		allowedDomain,
		jwtManager,
	)

	auditLogger := audit.NewLogger()
	proxyHandler := proxy.NewHandler(cfg, rbacManager, auditLogger)
	uiHandler := ui.NewHandler(rbacManager, cfg.AdminUsers)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Gateway's own endpoints under /.gateway/
	r.Route("/.gateway", func(r chi.Router) {
		// Health check (no auth required)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
		})

		// OAuth login endpoints (no auth required)
		r.Post("/login/start", handleLoginStart(oauthHandler))
		r.Post("/login/callback", handleLoginCallback(oauthHandler))

		// Authenticated gateway endpoints
		r.Group(func(r chi.Router) {
			r.Use(jwtManager.Middleware)
			
			r.Get("/me", handleMe())

			// Admin endpoints
			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin(cfg.AdminUsers))
				
				r.Get("/users", uiHandler.ListUsers)
				r.Post("/users", uiHandler.CreateUser)
				r.Put("/users/{email}", uiHandler.UpdateUser)
				r.Delete("/users/{email}", uiHandler.DeleteUser)

				r.Get("/roles", uiHandler.ListRoles)
				r.Post("/roles", uiHandler.CreateRole)
				r.Put("/roles/{name}", uiHandler.UpdateRole)
				r.Delete("/roles/{name}", uiHandler.DeleteRole)
			})
		})
	})

	// Keep /health at root for backwards compatibility (can be removed later)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.OptionalAuth(jwtManager))
		r.Get("/", uiHandler.Index)
		r.Get("/ui/*", uiHandler.ServeStatic)
	})

	// Catch-all: proxy everything else to the Convox rack
	// This MUST come last to avoid catching gateway routes
	r.Group(func(r chi.Router) {
		r.Use(jwtManager.Middleware)
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

func handleLoginStart(oauth *auth.OAuthHandler) http.HandlerFunc {
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

func handleLoginCallback(oauth *auth.OAuthHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code         string `json:"code"`
			State        string `json:"state"`
			CodeVerifier string `json:"code_verifier"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
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

func handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.GetUser(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"email": user.Email,
			"name":  user.Name,
		})
	}
}

func requireAdmin(adminUsers []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.GetUser(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			isAdmin := false
			for _, admin := range adminUsers {
				if user.Email == admin {
					isAdmin = true
					break
				}
			}

			if !isAdmin {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}