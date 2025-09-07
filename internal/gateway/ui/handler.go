package ui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/go-chi/chi/v5"
	"regexp"
)

type Handler struct {
	rbacManager  rbac.RBACManager
	configPath   string
	tokenService *token.Service
	database     *db.Database
	emailer      email.Sender
	rackName     string
}

func NewHandler(rbacManager rbac.RBACManager, configPath string, tokenService *token.Service, database *db.Database, mailer email.Sender, rackName string) *Handler {
	return &Handler{
		rbacManager:  rbacManager,
		configPath:   configPath,
		tokenService: tokenService,
		database:     database,
		emailer:      mailer,
		rackName:     rackName,
	}
}

var emailRegex = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

func isValidEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	return emailRegex.MatchString(email)
}

// auditUserAction records an audit log for a user-management action
func (h *Handler) auditUserAction(r *http.Request, action, resource, status string, details map[string]interface{}, start time.Time) {
	if h.database == nil {
		return
	}
	au, _ := auth.GetAuthUser(r.Context())
	var detailsJSON []byte
	if details != nil {
		detailsJSON, _ = json.Marshal(details)
	}
	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail: func() string {
			if au != nil {
				return au.Email
			}
			return ""
		}(),
		UserName: func() string {
			if au != nil {
				return au.Name
			}
			return ""
		}(),
		ActionType:     "user_management",
		Action:         action,
		Resource:       resource,
		Details:        string(detailsJSON),
		IPAddress:      r.RemoteAddr,
		UserAgent:      r.Header.Get("User-Agent"),
		Status:         status,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
	})
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

// GetCSRFToken issues a CSRF token (double-submit cookie) and returns it in JSON for the SPA.
func (h *Handler) GetCSRFToken(w http.ResponseWriter, r *http.Request) {
	tokenCookie, err := r.Cookie("csrf_token")
	token := ""
	if err != nil || tokenCookie == nil || tokenCookie.Value == "" {
		// Generate a random 32-byte token
		b := make([]byte, 32)
		if _, err := rand.Read(b); err == nil {
			token = hex.EncodeToString(b)
		}
		if token == "" {
			token = fmt.Sprintf("%d", time.Now().UnixNano())
		}
	} else {
		token = tokenCookie.Value
	}

	// Set cookie (not HttpOnly so SPA can read if needed; Lax to support redirects)
	secure := os.Getenv("COOKIE_SECURE") != "false" && os.Getenv("DEV_MODE") != "true"
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// ListAuditLogs returns audit logs (admin only). Minimal implementation returns an empty list
// while full audit storage is being implemented.
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "audit.list", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
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

	// Pagination params
	limit := 1000
	offset := 0
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			if v > 0 {
				if v > 1000 {
					v = 1000
				}
				limit = v
			}
		}
	}
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	// Pull logs from DB (filter by since) with pagination; filter others in-memory for now
	logs, err := h.database.GetAuditLogsPaged("", since, limit, offset)
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
		h.auditUserAction(r, "audit.export", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
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
	buf := "timestamp,user_email,user_name,action_type,action,command,resource,status,response_time_ms,ip_address,user_agent\n"
	for _, l := range logs {
		buf += fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%d,%s,%s\n",
			l.Timestamp.Format(time.RFC3339), escapeCSV(l.UserEmail), escapeCSV(l.UserName), l.ActionType, escapeCSV(l.Action), escapeCSV(l.Command), escapeCSV(l.Resource), l.Status, l.ResponseTimeMs, escapeCSV(l.IPAddress), escapeCSV(l.UserAgent))
	}
	_, _ = w.Write([]byte(buf))
}

func parseRange(s string) (time.Duration, error) {
	// Supports Nd or Nh (days or hours)
	if strings.HasSuffix(s, "m") {
		minsStr := strings.TrimSuffix(s, "m")
		m, err := strconv.Atoi(minsStr)
		if err != nil {
			return 0, err
		}
		return time.Duration(m) * time.Minute, nil
	}
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
		h.auditUserAction(r, "token.create", targetUserEmail, "error", map[string]interface{}{"name": req.Name}, time.Now())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResp)
	h.auditUserAction(r, "token.create", targetUserEmail, "success", map[string]interface{}{"name": req.Name}, time.Now())

	// Notifications
	if h.emailer != nil {
		inviter, _ := auth.GetAuthUser(r.Context())
		owner := targetUserEmail
		subjectOwner := fmt.Sprintf("Convox Gateway: New API token created on %s", h.rackName)
		bodyOwner := fmt.Sprintf("A new API token '%s' was created for your account on rack '%s'.\n\nCreated by: %s\nIf this wasn't expected, please contact an admin.", req.Name, h.rackName, func() string {
			if inviter != nil {
				return inviter.Email
			}
			return ""
		}())
		_ = h.emailer.Send(owner, subjectOwner, bodyOwner)

		admins := h.getAdminEmails()
		if len(admins) > 0 {
			subjectAdmin := fmt.Sprintf("Convox Gateway: API token created for %s on %s", owner, h.rackName)
			creator := "unknown"
			if inviter != nil {
				creator = inviter.Email
			}
			bodyAdmin := fmt.Sprintf("An API token '%s' was created for %s on rack '%s'.\nCreated by: %s", req.Name, owner, h.rackName, creator)
			_ = h.emailer.SendMany(orderByInviterFirst(admins, inviter), subjectAdmin, bodyAdmin)
		}
	}
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
		h.auditUserAction(r, "token.delete", tokenIDStr, "error", nil, time.Now())
		return
	}

	w.WriteHeader(http.StatusNoContent)
	h.auditUserAction(r, "token.delete", tokenIDStr, "success", nil, time.Now())
}

// ListUsers returns all users (admin only)
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.list", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
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
	start := time.Now()
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.create", "", "denied", map[string]interface{}{"reason": "forbidden"}, start)
		return
	}

	var req struct {
		Email string   `json:"email"`
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		h.auditUserAction(r, "user.create", "", "error", map[string]interface{}{"error": "invalid request body"}, start)
		return
	}

	// Basic email validation
	if !isValidEmail(req.Email) {
		http.Error(w, "invalid email format", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	existing, _ := h.rbacManager.GetUser(req.Email)
	if existing != nil {
		http.Error(w, "user already exists", http.StatusConflict)
		h.auditUserAction(r, "user.create", req.Email, "error", map[string]interface{}{"error": "user exists"}, start)
		return
	}

	// Create user
	if err := h.rbacManager.SaveUser(req.Email, &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}); err != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		h.auditUserAction(r, "user.create", req.Email, "error", map[string]interface{}{"error": "save failed"}, start)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	h.auditUserAction(r, "user.create", req.Email, "success", map[string]interface{}{"name": req.Name, "roles": req.Roles}, start)

	// Notifications
	if h.emailer != nil {
		inviter, _ := auth.GetAuthUser(r.Context())
		// To the new user
		subjectUser := fmt.Sprintf("You've been granted access to Convox Gateway for the %s rack", h.rackName)
		bodyUser := fmt.Sprintf("You've been added to the '%s' Convox rack.\n\nName: %s\nRoles: %s\nAdded by: %s", h.rackName, req.Name, strings.Join(req.Roles, ", "), func() string {
			if inviter != nil {
				return inviter.Email
			}
			return ""
		}())
		_ = h.emailer.Send(req.Email, subjectUser, bodyUser)

		// Notify admins (including inviter)
		admins := h.getAdminEmails()
		if len(admins) > 0 {
			subjectAdmin := fmt.Sprintf("Convox Gateway: New user added to %s", h.rackName)
			creator := "unknown"
			if inviter != nil {
				creator = inviter.Email
			}
			bodyAdmin := fmt.Sprintf("%s added new user %s (%s) to rack '%s' with roles: %s", creator, req.Email, req.Name, h.rackName, strings.Join(req.Roles, ", "))
			_ = h.emailer.SendMany(orderByInviterFirst(admins, inviter), subjectAdmin, bodyAdmin)
		}
	}
}

// getAdminEmails returns all user emails with admin role.
func (h *Handler) getAdminEmails() []string {
	users, err := h.rbacManager.GetUsers()
	if err != nil {
		return nil
	}
	emails := make([]string, 0)
	for emailAddr, u := range users {
		for _, r := range u.Roles {
			if r == "admin" {
				emails = append(emails, emailAddr)
				break
			}
		}
	}
	return emails
}

// orderByInviterFirst ensures inviter is primary recipient (first), useful when BCC-ing others.
func orderByInviterFirst(admins []string, inviter *auth.AuthUser) []string {
	if inviter == nil || len(admins) == 0 {
		return admins
	}
	idx := -1
	for i, a := range admins {
		if a == inviter.Email {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return admins
	}
	out := make([]string, 0, len(admins))
	out = append(out, inviter.Email)
	for i, a := range admins {
		if i == idx {
			continue
		}
		out = append(out, a)
	}
	return out
}

// DeleteUser removes a user (admin only)
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.delete", "", "denied", map[string]interface{}{"reason": "forbidden"}, start)
		return
	}

	email := chi.URLParam(r, "email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		h.auditUserAction(r, "user.delete", "", "error", map[string]interface{}{"error": "missing email"}, start)
		return
	}

	if err := h.rbacManager.DeleteUser(email); err != nil {
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		h.auditUserAction(r, "user.delete", email, "error", map[string]interface{}{"error": "delete failed"}, start)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	h.auditUserAction(r, "user.delete", email, "success", nil, start)
}

// UpdateUserRoles updates a user's roles (admin only)
func (h *Handler) UpdateUserRoles(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.update_roles", "", "denied", map[string]interface{}{"reason": "forbidden"}, start)
		return
	}

	email := chi.URLParam(r, "email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		h.auditUserAction(r, "user.update_roles", "", "error", map[string]interface{}{"error": "missing email"}, start)
		return
	}

	var req struct {
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		h.auditUserAction(r, "user.update_roles", email, "error", map[string]interface{}{"error": "invalid body"}, start)
		return
	}

	// Get existing user
	existing, err := h.rbacManager.GetUser(email)
	if err != nil || existing == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		h.auditUserAction(r, "user.update_roles", email, "error", map[string]interface{}{"error": "not found"}, start)
		return
	}

	// Update roles
	existing.Roles = req.Roles
	if err := h.rbacManager.SaveUser(email, existing); err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		h.auditUserAction(r, "user.update_roles", email, "error", map[string]interface{}{"error": "save failed"}, start)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	h.auditUserAction(r, "user.update_roles", email, "success", map[string]interface{}{"roles": req.Roles}, start)
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
