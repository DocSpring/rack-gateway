package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	rbacManager  rbac.RBACManager
	configPath   string
	tokenService *token.Service
	database     *db.Database
}

func NewHandler(rbacManager rbac.RBACManager, configPath string, tokenService *token.Service, database *db.Database) *Handler {
	return &Handler{
		rbacManager:  rbacManager,
		configPath:   configPath,
		tokenService: tokenService,
		database:     database,
	}
}

// GetConfig returns the current gateway configuration
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUser(r.Context())
	if !h.hasReadAccess(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Get users from the manager
	users, err := h.rbacManager.GetUsers()
	if err != nil {
		http.Error(w, "failed to get users", http.StatusInternalServerError)
		return
	}

	// Convert internal format to API format
	apiConfig := map[string]interface{}{
		"domain": h.rbacManager.GetAllowedDomain(),
		"users":  make(map[string]interface{}),
	}

	for email, user := range users {
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

	// Update users in the database
	for email, userConfig := range config.Users {
		if err := h.rbacManager.SaveUser(email, userConfig); err != nil {
			http.Error(w, "failed to save user", http.StatusInternalServerError)
			return
		}
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
	roles, err := h.rbacManager.GetUserRoles(user.Email)
	if err != nil {
		roles = []string{} // Default to empty if error
	}

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

// ListAuditLogs returns audit logs (admin only). Minimal implementation returns an empty list
// while full audit storage is being implemented.
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Parse filters
	q := r.URL.Query()
	rangeParam := q.Get("range") // e.g., 1h, 24h, 7d, 30d, all
	actionType := q.Get("action_type")
	status := q.Get("status")
	search := q.Get("search")

	// Compute since based on range
	since := time.Time{}
	if rangeParam != "" && rangeParam != "all" {
		dur, err := parseRange(rangeParam)
		if err == nil {
			since = time.Now().Add(-dur)
		}
	}

	// Pull logs from DB (filter by since); filter others in-memory for now
	logs, err := h.database.GetAuditLogs("", since, 0)
	if err != nil {
		http.Error(w, "failed to get audit logs", http.StatusInternalServerError)
		return
	}

	filtered := make([]*db.AuditLog, 0, len(logs))
	for _, l := range logs {
		if actionType != "" && actionType != "all" && l.ActionType != actionType {
			continue
		}
		if status != "" && status != "all" && l.Status != status {
			continue
		}
		if search != "" {
			if !containsAny([]string{l.UserEmail, l.UserName, l.Action, l.Resource, l.Details, l.IPAddress, l.UserAgent}, search) {
				continue
			}
		}
		filtered = append(filtered, l)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}

// ExportAuditLogs returns CSV export of audit logs (admin only). Minimal empty CSV.
func (h *Handler) ExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	rangeParam := q.Get("range")
	// Compute since
	since := time.Time{}
	if rangeParam != "" && rangeParam != "all" {
		if dur, err := parseRange(rangeParam); err == nil {
			since = time.Now().Add(-dur)
		}
	}

	logs, err := h.database.GetAuditLogs("", since, 0)
	if err != nil {
		http.Error(w, "failed to export audit logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audits.csv")
	buf := "timestamp,user_email,user_name,action_type,action,resource,status,response_time_ms,ip_address,user_agent\n"
	for _, l := range logs {
		buf += fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%d,%s,%s\n",
			l.Timestamp.Format(time.RFC3339), escapeCSV(l.UserEmail), escapeCSV(l.UserName), l.ActionType, escapeCSV(l.Action), escapeCSV(l.Resource), l.Status, l.ResponseTimeMs, escapeCSV(l.IPAddress), escapeCSV(l.UserAgent))
	}
	_, _ = w.Write([]byte(buf))
}

func parseRange(s string) (time.Duration, error) {
	// Supports Nd or Nh (days or hours)
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		d, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, err
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(s, "h") {
		hoursStr := strings.TrimSuffix(s, "h")
		h, err := strconv.Atoi(hoursStr)
		if err != nil {
			return 0, err
		}
		return time.Duration(h) * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid range")
}

func containsAny(fields []string, needle string) bool {
	n := strings.ToLower(needle)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), n) {
			return true
		}
	}
	return false
}

func escapeCSV(s string) string {
	if strings.ContainsAny(s, ",\n\r\"") {
		s = strings.ReplaceAll(s, "\"", "\"\"")
		return "\"" + s + "\""
	}
	return s
}

// CreateAPIToken creates a new API token
func (h *Handler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	authUser, isAuth := auth.GetAuthUser(r.Context())
	if !isAuth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Only admins can create tokens for other users
	var targetUserEmail string
	var req struct {
		Name        string   `json:"name"`
		UserEmail   string   `json:"user_email,omitempty"`
		Permissions []string `json:"permissions"`
		ExpiresAt   string   `json:"expires_at,omitempty"` // ISO8601 string
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Determine target user
	if req.UserEmail != "" {
		// Admin creating token for another user
		if !h.hasWriteAccess(&auth.Claims{Email: authUser.Email}) {
			http.Error(w, "forbidden: only admins can create tokens for other users", http.StatusForbidden)
			return
		}
		targetUserEmail = req.UserEmail
	} else {
		// User creating token for themselves
		targetUserEmail = authUser.Email
	}

	// Get target user from database
	user, err := h.rbacManager.GetUserWithID(targetUserEmail)
	if err != nil {
		http.Error(w, "failed to get user", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Parse expiry time
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at format (use ISO8601)", http.StatusBadRequest)
			return
		}
		expiresAt = &parsed
	} else {
		// No default expiry; tokens do not expire automatically
		expiresAt = token.DefaultTokenExpiry()
	}

	// Use default CICD permissions if none provided
	permissions := req.Permissions
	if len(permissions) == 0 {
		permissions = token.DefaultCICDPermissions()
	}

	// Create token
	tokenResp, err := h.tokenService.GenerateAPIToken(&token.APITokenRequest{
		Name:        req.Name,
		UserID:      user.ID,
		Permissions: permissions,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResp)
}

// ListAPITokens returns API tokens for the current user
func (h *Handler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	authUser, isAuth := auth.GetAuthUser(r.Context())
	if !isAuth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user ID from the database
	user, err := h.rbacManager.GetUserWithID(authUser.Email)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	tokens, err := h.tokenService.ListTokensForUser(user.ID)
	if err != nil {
		http.Error(w, "failed to list tokens", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokens)
}

// DeleteAPIToken deletes an API token
func (h *Handler) DeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	authUser, isAuth := auth.GetAuthUser(r.Context())
	if !isAuth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	tokenIDStr := chi.URLParam(r, "tokenID")
	tokenID, err := strconv.ParseInt(tokenIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid token ID", http.StatusBadRequest)
		return
	}

	// TODO: Verify user owns this token or is admin
	// For now, allow deletion (should check ownership)
	_ = authUser // Suppress unused variable warning
	if err := h.tokenService.DeleteToken(tokenID); err != nil {
		http.Error(w, "failed to delete token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListUsers returns all users (admin only)
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	users, err := h.rbacManager.GetUsers()
	if err != nil {
		http.Error(w, "failed to get users", http.StatusInternalServerError)
		return
	}

	// Convert map to slice for easier consumption
	userList := make([]map[string]interface{}, 0)
	for email, user := range users {
		userList = append(userList, map[string]interface{}{
			"email": email,
			"name":  user.Name,
			"roles": user.Roles,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userList)
}

// CreateUser creates a new user (admin only)
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Email string   `json:"email"`
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	existing, _ := h.rbacManager.GetUser(req.Email)
	if existing != nil {
		http.Error(w, "user already exists", http.StatusConflict)
		return
	}

	// Create user
	if err := h.rbacManager.SaveUser(req.Email, &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}); err != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

// DeleteUser removes a user (admin only)
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	email := chi.URLParam(r, "email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	if err := h.rbacManager.DeleteUser(email); err != nil {
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateUserRoles updates a user's roles (admin only)
func (h *Handler) UpdateUserRoles(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	email := chi.URLParam(r, "email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	var req struct {
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Get existing user
	existing, err := h.rbacManager.GetUser(email)
	if err != nil || existing == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Update roles
	existing.Roles = req.Roles
	if err := h.rbacManager.SaveUser(email, existing); err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
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

	roles, err := h.rbacManager.GetUserRoles(user.Email)
	if err != nil {
		return false
	}
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

	roles, err := h.rbacManager.GetUserRoles(user.Email)
	if err != nil {
		return false
	}
	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

func (h *Handler) isAdmin(r *http.Request) bool {
	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		return false
	}

	roles, err := h.rbacManager.GetUserRoles(authUser.Email)
	if err != nil {
		return false
	}

	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}
