package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	config      *config.Config
	rbacManager rbac.RBACManager
	auditLogger *audit.Logger
}

func NewHandler(cfg *config.Config, rbacManager rbac.RBACManager, auditLogger *audit.Logger) *Handler {
	return &Handler{
		config:      cfg,
		rbacManager: rbacManager,
		auditLogger: auditLogger,
	}
}

// ProxyToRack handles all requests that should be proxied to the Convox rack
func (h *Handler) ProxyToRack(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Get the default rack (there's only one per gateway instance)
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		// Try local rack in dev mode
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			h.handleError(w, r, "no rack configured", http.StatusInternalServerError, "default", start)
			return
		}
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rackConfig.Name, start)
		return
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rackConfig.Name, start)
		return
	}

	// Get the full path including query params
	path := r.URL.Path

	// Check permissions (different logic for JWT vs API tokens)
	var allowed bool
	var err error
	resource, action := h.pathToResourceAction(path, r.Method)

	if authUser.IsAPIToken {
		// For API tokens, check permissions directly
		allowed = h.hasAPITokenPermission(authUser, resource, action)
	} else {
		// For JWT users, use RBAC
		allowed, err = h.rbacManager.Enforce(authUser.Email, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	// Forward the request to the rack
	if err := h.forwardRequest(w, r, rackConfig, path, authUser.Email); err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rackConfig.Name, start)
		return
	}

	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "allow", http.StatusOK, time.Since(start), nil)
}

func (h *Handler) ProxyRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	rack := chi.URLParam(r, "rack")
	path := chi.URLParam(r, "*")

	rackConfig, exists := h.config.Racks[rack]
	if !exists {
		h.handleError(w, r, "unknown rack", http.StatusNotFound, rack, start)
		return
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rack, start)
		return
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rack, start)
		return
	}

	var allowed bool
	var err error
	resource, action := h.pathToResourceAction(path, r.Method)

	if authUser.IsAPIToken {
		// For API tokens, check permissions directly
		allowed = h.hasAPITokenPermission(authUser, resource, action)
	} else {
		// For JWT users, use RBAC
		allowed, err = h.rbacManager.Enforce(authUser.Email, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rack, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	if err := h.forwardRequest(w, r, rackConfig, path, authUser.Email); err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rack, start)
		return
	}

	h.auditLogger.LogRequest(r, authUser.Email, rack, "allow", http.StatusOK, time.Since(start), nil)
}

func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, rack config.RackConfig, path, userEmail string) error {
	targetURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(rack.URL, "/"), path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
		r.Body.Close()
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	for key, values := range r.Header {
		if key != "Authorization" {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}
	}

	// Convox uses Basic Auth with configurable username (default "convox") and the API key as password
	proxyReq.Header.Set("Authorization", fmt.Sprintf("Basic %s",
		base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	proxyReq.Header.Set("X-User-Email", userEmail)
	proxyReq.Header.Set("X-Request-ID", uuid.New().String())

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		return fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}

// pathToResourceAction converts a path and HTTP method to resource and action for RBAC
func (h *Handler) pathToResourceAction(path, method string) (string, string) {
	permission := h.mapPathToPermission(method, path)
	// permission format is "convox:resource:action"
	parts := strings.Split(permission, ":")
	if len(parts) == 3 {
		return parts[1], parts[2]
	}
	return "*", "*"
}

func (h *Handler) mapPathToPermission(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return "convox:unknown:unknown"
	}

	resource := parts[0]
	action := "read"

	switch method {
	case "POST":
		action = "create"
	case "PUT", "PATCH":
		action = "update"
	case "DELETE":
		action = "delete"
	}

	if strings.Contains(path, "/env") {
		if method == "GET" {
			return "convox:env:get"
		}
		return "convox:env:set"
	}

	if strings.Contains(path, "/ps") {
		if method == "GET" {
			return "convox:ps:list"
		}
		return "convox:ps:manage"
	}

	if strings.Contains(path, "/apps") {
		if method == "GET" {
			return "convox:apps:list"
		}
		return "convox:apps:manage"
	}

	if strings.Contains(path, "/run") {
		return "convox:run:command"
	}

	if strings.Contains(path, "/restart") {
		return "convox:restart:app"
	}

	return fmt.Sprintf("convox:%s:%s", resource, action)
}

// hasAPITokenPermission checks if an API token has the required permission
func (h *Handler) hasAPITokenPermission(authUser *auth.AuthUser, resource, action string) bool {
	permission := fmt.Sprintf("convox:%s:%s", resource, action)

	for _, perm := range authUser.Permissions {
		// Check for exact match
		if perm == permission {
			return true
		}
		// Check for wildcard matches
		if perm == "convox:*:*" || perm == fmt.Sprintf("convox:%s:*", resource) {
			return true
		}
	}

	return false
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, message string, status int, rack string, start time.Time) {
	userEmail := "anonymous"
	if authUser, ok := auth.GetAuthUser(r.Context()); ok {
		userEmail = authUser.Email
	}

	h.auditLogger.LogRequest(r, userEmail, rack, "error", status, time.Since(start), fmt.Errorf("%s", message))

	errorResponse := map[string]string{"error": message}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse)
}
