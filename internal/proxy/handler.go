package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docspring/convox-gateway/internal/audit"
	"github.com/docspring/convox-gateway/internal/auth"
	"github.com/docspring/convox-gateway/internal/config"
	"github.com/docspring/convox-gateway/internal/rbac"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	config      *config.Config
	rbacManager *rbac.Manager
	auditLogger *audit.Logger
}

func NewHandler(cfg *config.Config, rbacManager *rbac.Manager, auditLogger *audit.Logger) *Handler {
	return &Handler{
		config:      cfg,
		rbacManager: rbacManager,
		auditLogger: auditLogger,
	}
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

	user, ok := auth.GetUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rack, start)
		return
	}

	permission := h.mapPathToPermission(r.Method, path)
	if !h.rbacManager.CheckPermission(user.Email, path, r.Method) {
		h.auditLogger.LogRequest(r, user.Email, rack, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied: %s", permission))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	if err := h.forwardRequest(w, r, rackConfig, path, user.Email); err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rack, start)
		return
	}

	h.auditLogger.LogRequest(r, user.Email, rack, "allow", http.StatusOK, time.Since(start), nil)
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

	proxyReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rack.APIKey))
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

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, message string, status int, rack string, start time.Time) {
	userEmail := "anonymous"
	if user, ok := auth.GetUser(r.Context()); ok {
		userEmail = user.Email
	}

	h.auditLogger.LogRequest(r, userEmail, rack, "error", status, time.Since(start), fmt.Errorf(message))

	errorResponse := map[string]string{"error": message}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse)
}