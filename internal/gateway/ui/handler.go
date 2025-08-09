package ui

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"gopkg.in/yaml.v3"
)

type Handler struct {
	rbacManager *rbac.Manager
	configPath  string
	staticFS    fs.FS
}

func NewHandler(rbacManager *rbac.Manager, configPath string) *Handler {
	return &Handler{
		rbacManager: rbacManager,
		configPath:  configPath,
	}
}

// GetConfig returns the current gateway configuration
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUser(r.Context())
	if !h.hasReadAccess(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	config := h.rbacManager.GetConfig()
	if config == nil {
		// Return empty config
		config = &rbac.GatewayConfig{
			Domain: h.rbacManager.GetDomain(),
			Users:  make(map[string]*rbac.UserConfig),
		}
	}

	// Convert internal format to API format
	apiConfig := map[string]interface{}{
		"domain": config.Domain,
		"users":  make(map[string]interface{}),
	}

	for email, user := range config.Users {
		apiConfig["users"].(map[string]interface{})[email] = map[string]interface{}{
			"name":  user.Name,
			"roles": user.Roles,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiConfig)
}

// UpdateConfig updates the entire gateway configuration
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUser(r.Context())
	if !h.hasWriteAccess(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var config rbac.GatewayConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Save to file
	if err := h.saveConfigToFile(&config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reload in RBAC manager
	if err := h.rbacManager.LoadConfigs(); err != nil {
		http.Error(w, "failed to reload config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// GetMe returns the current user's information
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, isAuth := auth.GetUser(r.Context())
	if !isAuth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user's roles from RBAC
	roles := h.rbacManager.GetUserRoles(user.Email)

	response := map[string]interface{}{
		"email": user.Email,
		"name":  user.Name,
		"roles": roles,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListRoles returns the hardcoded roles
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles := map[string]interface{}{
		"viewer": map[string]interface{}{
			"name":        "viewer",
			"description": "Read-only access to apps, processes, and logs",
			"permissions": []string{"convox:apps:list", "convox:ps:list", "convox:logs:view"},
		},
		"ops": map[string]interface{}{
			"name":        "ops",
			"description": "Restart apps, view environments, manage processes",
			"permissions": []string{"convox:apps:*", "convox:ps:*", "convox:env:get", "convox:logs:*"},
		},
		"deployer": map[string]interface{}{
			"name":        "deployer",
			"description": "Full deployment permissions including env vars",
			"permissions": []string{"convox:*:*"},
		},
		"admin": map[string]interface{}{
			"name":        "admin",
			"description": "Complete access to all operations",
			"permissions": []string{"*"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(roles)
}

// Health check endpoint
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": "1.0.0",
	})
}

// ServeStatic serves the React app's static files
func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	// In production, serve from embedded files or dist directory
	// For development, Vite dev server handles this
	staticDir := "web/dist"
	if _, err := os.Stat(staticDir); err == nil {
		http.FileServer(http.Dir(staticDir)).ServeHTTP(w, r)
	} else {
		http.NotFound(w, r)
	}
}

// Helper functions
func (h *Handler) hasReadAccess(user *auth.Claims) bool {
	if user == nil {
		return false
	}

	roles := h.rbacManager.GetUserRoles(user.Email)
	for _, role := range roles {
		if role == "admin" || role == "ops" || role == "deployer" {
			return true
		}
	}
	return false
}

func (h *Handler) hasWriteAccess(user *auth.Claims) bool {
	if user == nil {
		return false
	}

	roles := h.rbacManager.GetUserRoles(user.Email)
	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

func (h *Handler) saveConfigToFile(config *rbac.GatewayConfig) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(h.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(h.configPath, data, 0644)
}
