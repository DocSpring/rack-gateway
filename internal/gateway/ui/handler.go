package ui

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"regexp"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	emailtemplates "github.com/DocSpring/convox-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	rbacManager  rbac.RBACManager
	configPath   string
	tokenService *token.Service
	database     *db.Database
	emailer      email.Sender
	rackName     string
	rackConfig   config.RackConfig
	devProxy     *httputil.ReverseProxy
	publicBase   string
}

func NewHandler(rbacManager rbac.RBACManager, configPath string, tokenService *token.Service, database *db.Database, mailer email.Sender, rackName string, rackCfg config.RackConfig, devProxyURL string, publicBase string) *Handler {
	var rp *httputil.ReverseProxy
	if devProxyURL != "" {
		if u, err := url.Parse(devProxyURL); err == nil {
			rp = httputil.NewSingleHostReverseProxy(u)
		}
	}
	return &Handler{
		rbacManager:  rbacManager,
		configPath:   configPath,
		tokenService: tokenService,
		database:     database,
		emailer:      mailer,
		rackName:     rackName,
		rackConfig:   rackCfg,
		devProxy:     rp,
		publicBase:   publicBase,
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
	// Infer action type and resource type for admin actions
	actionType := "users"
	if strings.HasPrefix(action, "api_token.") {
		actionType = "tokens"
	} else if strings.HasPrefix(action, "user.") {
		actionType = "users"
	}
	// Infer resource type for user/token management actions
	resourceType := func(a string) string {
		if strings.HasPrefix(a, "api_token.") {
			return "api_token"
		}
		if strings.HasPrefix(a, "user.") {
			return "user"
		}
		return "admin"
	}(action)

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
		ActionType:     actionType,
		Action:         action,
		Resource:       resource,
		ResourceType:   resourceType,
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

// GetSettings returns gateway settings stored in the database
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	au, ok := auth.GetAuthUser(r.Context())
	if !ok || !h.hasReadAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	resp := map[string]interface{}{}
	if h.database != nil {
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			resp["protected_env_vars"] = arr
		} else {
			resp["protected_env_vars"] = []string{}
		}
		if v, err := h.database.GetAllowDestructiveActions(); err == nil {
			resp["allow_destructive_actions"] = v
		} else {
			resp["allow_destructive_actions"] = false
		}
	} else {
		resp["protected_env_vars"] = []string{}
		resp["allow_destructive_actions"] = false
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateProtectedEnvVars updates the protected env var names in the settings table.
func (h *Handler) UpdateProtectedEnvVars(w http.ResponseWriter, r *http.Request) {
	au, ok := auth.GetAuthUser(r.Context())
	if !ok || !h.hasWriteAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload struct {
		Protected []string `json:"protected_env_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Normalize and de-dup to uppercase
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.Protected))
	for _, k := range payload.Protected {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	// Determine updating user id if available
	var uid *int64
	if h.rbacManager != nil {
		if u, err := h.rbacManager.GetUserWithID(au.Email); err == nil && u != nil {
			uid = &u.ID
		}
	}
	if h.database != nil {
		if err := h.database.UpsertSetting("protected_env_vars", out, uid); err != nil {
			http.Error(w, "failed to save setting", http.StatusInternalServerError)
			return
		}
		// Emit a setting:changed audit event
		_ = audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    au.Email,
			UserName:     r.Header.Get("X-User-Name"),
			ActionType:   "settings",
			Action:       "setting.changed",
			ResourceType: "setting",
			Resource:     "protected_env_vars",
			Details: func() string {
				b, _ := json.Marshal(map[string]interface{}{"protected_env_vars": out})
				return string(b)
			}(),
			IPAddress:      r.RemoteAddr,
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     http.StatusOK,
			ResponseTimeMs: 0,
		})

		// Email admins about the settings change
		if h.emailer != nil {
			admins := h.getAdminEmails()
			if len(admins) > 0 {
				subject := fmt.Sprintf("Convox Gateway (%s): %s changed the %s setting", h.rackName, au.Email, "protected_env_vars")
				value := strings.Join(out, ", ")
				text, html, _ := emailtemplates.RenderSettingsChanged(h.rackName, au.Email, "protected_env_vars", value)
				_ = h.emailer.SendMany(orderByInviterFirst(admins, au), subject, text, html)
			}
		}
	}
	// Update in-memory cache for current handler
	if h.database != nil {
		// reload from db to be safe
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			// Note: proxy handler maintains its own cache; it will pick up on next restart.
			_ = arr // no local cache here; provided via proxy handler at init
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// UpdateAllowDestructiveActions updates the allow_destructive_actions setting.
func (h *Handler) UpdateAllowDestructiveActions(w http.ResponseWriter, r *http.Request) {
	au, ok := auth.GetAuthUser(r.Context())
	if !ok || !h.hasWriteAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var payload struct {
		Allow bool `json:"allow_destructive_actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var uid *int64
	if h.rbacManager != nil {
		if u, err := h.rbacManager.GetUserWithID(au.Email); err == nil && u != nil {
			uid = &u.ID
		}
	}
	if h.database != nil {
		if err := h.database.UpsertSetting("allow_destructive_actions", payload.Allow, uid); err != nil {
			http.Error(w, "failed to save setting", http.StatusInternalServerError)
			return
		}
		// Audit
		_ = audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    au.Email,
			UserName:     r.Header.Get("X-User-Name"),
			ActionType:   "settings",
			Action:       "setting.changed",
			ResourceType: "setting",
			Resource:     "allow_destructive_actions",
			Details: func() string {
				b, _ := json.Marshal(map[string]bool{"allow_destructive_actions": payload.Allow})
				return string(b)
			}(),
			IPAddress:      r.RemoteAddr,
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     http.StatusOK,
			ResponseTimeMs: 0,
		})
		// Email admins
		if h.emailer != nil {
			admins := h.getAdminEmails()
			if len(admins) > 0 {
				subject := fmt.Sprintf("Convox Gateway (%s): %s changed the %s setting", h.rackName, au.Email, "allow_destructive_actions")
				val := fmt.Sprintf("%t", payload.Allow)
				text, html, _ := emailtemplates.RenderSettingsChanged(h.rackName, au.Email, "allow_destructive_actions", val)
				_ = h.emailer.SendMany(orderByInviterFirst(admins, au), subject, text, html)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// UpdateConfig updates the entire gateway configuration
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUser(r.Context())
	if !h.hasWriteAccess(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Accept a minimal payload with users map; domain is read-only here
	var payload struct {
		Users map[string]*rbac.UserConfig `json:"users"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Update users in the database
	for email, userConfig := range payload.Users {
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

	rackName := os.Getenv("RACK")
	if rackName == "" {
		rackName = h.rackName
	}
	rackHost := os.Getenv("RACK_HOST")
	response := map[string]interface{}{
		"email": user.Email,
		"name":  user.Name,
		"roles": roles,
		"rack": map[string]string{
			"name": rackName,
			"host": rackHost,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetRackInfo fetches rack /system info from the upstream rack and returns it.
func (h *Handler) GetRackInfo(w http.ResponseWriter, r *http.Request) {
	au, isAuth := auth.GetAuthUser(r.Context())
	if !isAuth || !h.hasReadAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Build upstream URL
	base := strings.TrimRight(h.rackConfig.URL, "/")
	url := base + "/system"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	// Basic auth to rack
	user := h.rackConfig.Username
	if user == "" {
		user = "convox"
	}
	authz := base64.StdEncoding.EncodeToString([]byte(user + ":" + h.rackConfig.APIKey))
	req.Header.Set("Authorization", "Basic "+authz)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to fetch rack info", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		http.Error(w, string(b), resp.StatusCode)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
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
			"permissions": []string{"convox:apps:*", "convox:ps:*", "convox:releases:list", "convox:env:view", "convox:logs:*"},
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

// GetEnvValues returns the latest release env for an app.
// Query params: app (required), key (optional), secrets=true|false (optional)
func (h *Handler) GetEnvValues(w http.ResponseWriter, r *http.Request) {
	au, ok := auth.GetAuthUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	app := r.URL.Query().Get("app")
	if strings.TrimSpace(app) == "" {
		http.Error(w, "missing app", http.StatusBadRequest)
		return
	}
	key := r.URL.Query().Get("key")
	wantSecrets := strings.EqualFold(r.URL.Query().Get("secrets"), "true")

	// Enforce env:view
	if ok, _ := h.rbacManager.Enforce(au.Email, "env", "view"); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// If requesting secrets, enforce secrets:view
	if wantSecrets {
		if ok, _ := h.rbacManager.Enforce(au.Email, "secrets", "view"); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Fetch latest env via rack API using configured rack
	rc := h.rackConfig
	if rc.URL == "" || rc.APIKey == "" {
		http.Error(w, "rack not configured", http.StatusInternalServerError)
		return
	}
	envMap, err := envutil.FetchLatestEnvMap(rc, app)
	if err != nil {
		http.Error(w, "failed to fetch env", http.StatusBadGateway)
		return
	}

	// Mask if secrets not requested and log secrets.view when secrets requested
	if wantSecrets {
		// Audit secrets.view read
		_ = audit.LogDB(h.database, &db.AuditLog{
			UserEmail:    au.Email,
			UserName:     au.Name,
			ActionType:   "convox",
			Action:       "secrets.view",
			ResourceType: "secret",
			Resource: func() string {
				if key != "" {
					return fmt.Sprintf("%s/%s", app, key)
				} else {
					return "all"
				}
			}(),
			Details:        "{}",
			IPAddress:      r.RemoteAddr,
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     http.StatusOK,
			ResponseTimeMs: 0,
		})
	} else {
		extra := strings.Split(os.Getenv("CONVOX_SECRET_ENV_VARS"), ",")
		for k := range envMap {
			if envutil.IsSecretKey(k, extra) {
				envMap[k] = envutil.MaskedSecret
			}
		}
	}
	// Filter by key if provided
	if key != "" {
		v := envMap[key]
		envMap = map[string]string{key: v}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"env": envMap})
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
	// Allow any authenticated role (admin, deployer, ops, viewer) to view audit logs
	au, ok := auth.GetAuthUser(r.Context())
	if !ok || !h.hasReadAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "audit.list", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
		return
	}

	// Parse filters
	q := r.URL.Query()
	rangeParam := q.Get("range") // e.g., 1h, 24h, 7d, 30d, all
	actionType := q.Get("action_type")
	status := q.Get("status")
	resourceType := q.Get("resource_type")
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
		if resourceType != "" && resourceType != "all" && l.ResourceType != resourceType {
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
	// Allow any authenticated role to export
	au, ok := auth.GetAuthUser(r.Context())
	if !ok || !h.hasReadAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "audit.export", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
		return
	}

	q := r.URL.Query()
	rangeParam := q.Get("range")
	actionType := q.Get("action_type")
	status := q.Get("status")
	resourceType := q.Get("resource_type")
	search := q.Get("search")
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

	// Apply same filters as ListAuditLogs
	filtered := make([]*db.AuditLog, 0, len(logs))
	for _, l := range logs {
		if actionType != "" && actionType != "all" && l.ActionType != actionType {
			continue
		}
		if status != "" && status != "all" && l.Status != status {
			continue
		}
		if resourceType != "" && resourceType != "all" && l.ResourceType != resourceType {
			continue
		}
		if search != "" {
			if !containsAny([]string{l.UserEmail, l.UserName, l.Action, l.Resource, l.Details, l.IPAddress, l.UserAgent}, search) {
				continue
			}
		}
		filtered = append(filtered, l)
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audits.csv")
	buf := "timestamp,user_email,user_name,action_type,action,command,resource,status,response_time_ms,ip_address,user_agent\n"
	for _, l := range filtered {
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
		// Only admins and deployers can create tokens for themselves
		roles, _ := h.rbacManager.GetUserRoles(authUser.Email)
		isDeployer := false
		isAdmin := false
		for _, role := range roles {
			if role == "admin" {
				isAdmin = true
			}
			if role == "deployer" {
				isDeployer = true
			}
		}
		if !(isAdmin || isDeployer) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
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

	// Lookup creator user ID (who is making this request)
	var creatorID *int64
	if cu, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && cu != nil {
		creatorID = &cu.ID
	}

	// Create token
	tokenResp, err := h.tokenService.GenerateAPIToken(&token.APITokenRequest{
		Name:            req.Name,
		UserID:          user.ID,
		Permissions:     permissions,
		ExpiresAt:       expiresAt,
		CreatedByUserID: creatorID,
	})
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		h.auditUserAction(r, "api_token.create", targetUserEmail, "error", map[string]interface{}{"name": req.Name}, time.Now())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResp)
	// Use "<name> (id: <id>)" as the resource for clarity
	h.auditUserAction(
		r,
		"api_token.create",
		fmt.Sprintf("%d", tokenResp.APIToken.ID),
		"success",
		map[string]interface{}{"name": req.Name},
		time.Now(),
	)

	// Notifications
	if h.emailer != nil {
		inviter, _ := auth.GetAuthUser(r.Context())
		owner := targetUserEmail
		subjectOwner := fmt.Sprintf("Convox Gateway (%s): New API token created", h.rackName)
		creator := ""
		if inviter != nil {
			creator = inviter.Email
		}
		txt, html, _ := emailtemplates.RenderTokenCreatedOwner(h.rackName, req.Name, creator)
		_ = h.emailer.Send(owner, subjectOwner, txt, html)

		admins := h.getAdminEmails()
		// Do not email the owner twice if they are also an admin
		filteredAdmins := make([]string, 0, len(admins))
		for _, a := range admins {
			if strings.EqualFold(a, owner) {
				continue
			}
			filteredAdmins = append(filteredAdmins, a)
		}
		if len(filteredAdmins) > 0 {
			subjectAdmin := fmt.Sprintf("Convox Gateway (%s): API token created for %s", h.rackName, owner)
			creator := ""
			if inviter != nil {
				creator = inviter.Email
			}
			txt, html, _ := emailtemplates.RenderTokenCreatedAdmin(h.rackName, req.Name, owner, creator)
			_ = h.emailer.SendMany(orderByInviterFirst(filteredAdmins, inviter), subjectAdmin, txt, html)
		}
	}
}

// ListAPITokens returns API tokens for the current user
func (h *Handler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	au, isAuth := auth.GetAuthUser(r.Context())
	if !isAuth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// All authenticated users can view all API tokens (read-only; edit/delete guarded elsewhere)
	_ = au
	tokens, err := h.tokenService.ListAllTokens()
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

	// Verify ownership or admin
	isAdmin := h.isAdmin(r)
	ownerID := int64(0)
	if u, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && u != nil {
		ownerID = u.ID
	}
	owns := false
	if ownerID != 0 {
		if toks, err := h.tokenService.ListTokensForUser(ownerID); err == nil {
			for _, t := range toks {
				if t.ID == tokenID {
					owns = true
					break
				}
			}
		}
	}
	if !(isAdmin || owns) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := h.tokenService.DeleteToken(tokenID); err != nil {
		http.Error(w, "failed to delete token", http.StatusInternalServerError)
		h.auditUserAction(r, "api_token.delete", tokenIDStr, "error", nil, time.Now())
		return
	}

	w.WriteHeader(http.StatusNoContent)
	// Include name in details if we can resolve it post-delete context (best-effort before delete)
	var tName string
	if toks, err := h.tokenService.ListAllTokens(); err == nil {
		for _, t := range toks {
			if t.ID == tokenID {
				tName = t.Name
				break
			}
		}
	}
	det := map[string]interface{}{}
	if strings.TrimSpace(tName) != "" {
		det["name"] = tName
	}
	h.auditUserAction(r, "api_token.delete", tokenIDStr, "success", det, time.Now())
}

// UpdateAPITokenName updates the name of an API token
func (h *Handler) UpdateAPITokenName(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Verify ownership or admin
	isAdmin := h.isAdmin(r)
	ownerID := int64(0)
	if u, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && u != nil {
		ownerID = u.ID
	}
	owns := false
	if ownerID != 0 {
		if toks, err := h.tokenService.ListTokensForUser(ownerID); err == nil {
			for _, t := range toks {
				if t.ID == tokenID {
					owns = true
					break
				}
			}
		}
	}
	if !(isAdmin || owns) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := h.tokenService.UpdateTokenName(tokenID, req.Name); err != nil {
		http.Error(w, "failed to update token", http.StatusInternalServerError)
		h.auditUserAction(r, "api_token.update", tokenIDStr, "error", map[string]interface{}{"name": req.Name}, time.Now())
		return
	}

	w.WriteHeader(http.StatusNoContent)
	// Use id as resource; include new name in details
	h.auditUserAction(r, "api_token.update", tokenIDStr, "success", map[string]interface{}{"name": req.Name}, time.Now())
}

// ListUsers returns all users (admin only)
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	// Admin: full; Deployer: read-only list; others: forbidden
	au, ok := auth.GetAuthUser(r.Context())
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.list", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
		return
	}
	if !h.hasReadAccess(&auth.Claims{Email: au.Email}) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.list", "", "denied", map[string]interface{}{"reason": "forbidden"}, time.Now())
		return
	}

	var userList []map[string]interface{}
	if h.database != nil {
		// Prefer full details from DB when available
		dbUsers, err := h.database.ListUsers()
		if err != nil {
			http.Error(w, "failed to get users", http.StatusInternalServerError)
			return
		}
		userList = make([]map[string]interface{}, 0, len(dbUsers))
		// Optionally derive "added by" from audit logs (first user.create for this email)
		sqlDB := h.database.DB()
		for _, u := range dbUsers {
			var addedByEmail, addedByName sql.NullString
			if sqlDB != nil {
				_ = sqlDB.QueryRow(
					`SELECT user_email, user_name
                     FROM audit_logs
                     WHERE action_type = 'users' AND action = 'user.create' AND resource = $1
                     ORDER BY timestamp ASC
                     LIMIT 1`, fmt.Sprintf("%d", u.ID),
				).Scan(&addedByEmail, &addedByName)
			}
			userList = append(userList, map[string]interface{}{
				"email":      u.Email,
				"name":       u.Name,
				"roles":      u.Roles,
				"created_at": u.CreatedAt,
				"updated_at": u.UpdatedAt,
				"suspended":  u.Suspended,
				"created_by_email": func() string {
					if addedByEmail.Valid {
						return addedByEmail.String
					}
					return ""
				}(),
				"created_by_name": func() string {
					if addedByName.Valid {
						return addedByName.String
					}
					return ""
				}(),
			})
		}
	} else {
		// Fallback to RBAC-only data if DB not provided (tests)
		users, err := h.rbacManager.GetUsers()
		if err != nil {
			http.Error(w, "failed to get users", http.StatusInternalServerError)
			return
		}
		userList = make([]map[string]interface{}, 0, len(users))
		for email, user := range users {
			userList = append(userList, map[string]interface{}{
				"email": email,
				"name":  user.Name,
				"roles": user.Roles,
			})
		}
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
	// Resolve user ID for audit resource; include email in details
	createdUser, _ := h.rbacManager.GetUserWithID(req.Email)
	res := req.Email
	if createdUser != nil && createdUser.ID > 0 {
		res = fmt.Sprintf("%d", createdUser.ID)
	}
	h.auditUserAction(r, "user.create", res, "success", map[string]interface{}{"email": req.Email, "roles": req.Roles}, start)

	// Notifications
	if h.emailer != nil {
		inviter, _ := auth.GetAuthUser(r.Context())
		// To the new user
		subjectUser := fmt.Sprintf("Convox Gateway (%s): You've been granted access", h.rackName)
		inviterEmail := ""
		if inviter != nil {
			inviterEmail = inviter.Email
		}
		// Use public base for web and CLI (dev/prod aware)
		txt, html, _ := emailtemplates.RenderWelcome(h.rackName, req.Email, inviterEmail, h.publicBase, h.publicBase)
		_ = h.emailer.Send(req.Email, subjectUser, txt, html)

		// Notify admins (including inviter), but do not duplicate the end-user notification
		admins := h.getAdminEmails()
		filteredAdmins := make([]string, 0, len(admins))
		for _, a := range admins {
			if strings.EqualFold(a, req.Email) {
				continue
			}
			filteredAdmins = append(filteredAdmins, a)
		}
		if len(filteredAdmins) > 0 {
			creator := ""
			if inviter != nil {
				creator = inviter.Email
			}
			subjectAdmin := fmt.Sprintf("Convox Gateway (%s): %s added %s (%s)", h.rackName, creator, req.Email, req.Name)
			rolesStr := strings.Join(req.Roles, ", ")
			txt, html, _ := emailtemplates.RenderUserAddedAdmin(h.rackName, creator, req.Email, req.Name, req.Roles)
			// In case of error above, fallback simple body
			if txt == "" {
				txt = fmt.Sprintf("%s added new user %s (%s) with roles: %s.", creator, req.Email, req.Name, rolesStr)
			}
			_ = h.emailer.SendMany(orderByInviterFirst(filteredAdmins, inviter), subjectAdmin, txt, html)
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

	// Fetch ID before deletion for audit clarity
	delUser, _ := h.rbacManager.GetUserWithID(email)
	if err := h.rbacManager.DeleteUser(email); err != nil {
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		h.auditUserAction(r, "user.delete", func() string {
			if delUser != nil {
				return fmt.Sprintf("%d", delUser.ID)
			}
			return email
		}(), "error", map[string]interface{}{"error": "delete failed", "email": email}, start)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	h.auditUserAction(r, "user.delete", func() string {
		if delUser != nil {
			return fmt.Sprintf("%d", delUser.ID)
		}
		return email
	}(), "success", map[string]interface{}{"email": email}, start)
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
	// Resolve user ID for audit resource; include email in details
	updUser, _ := h.rbacManager.GetUserWithID(email)
	h.auditUserAction(r, "user.update_roles", func() string {
		if updUser != nil {
			return fmt.Sprintf("%d", updUser.ID)
		}
		return email
	}(), "success", map[string]interface{}{"email": email, "roles": req.Roles}, start)
}

// UpdateUserProfile updates a user's name and/or email (admin only)
func (h *Handler) UpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		h.auditUserAction(r, "user.update", "", "denied", map[string]interface{}{"reason": "forbidden"}, start)
		return
	}
	oldEmail := chi.URLParam(r, "email")
	if strings.TrimSpace(oldEmail) == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		h.auditUserAction(r, "user.update", "", "error", map[string]interface{}{"error": "missing email"}, start)
		return
	}
	var req struct {
		Email *string `json:"email"`
		Name  *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		h.auditUserAction(r, "user.update", oldEmail, "error", map[string]interface{}{"error": "invalid body"}, start)
		return
	}
	// Load existing directly from DB to avoid any cache staleness
	if h.database == nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	dbUser, err := h.database.GetUser(oldEmail)
	if err != nil || dbUser == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		h.auditUserAction(r, "user.update", oldEmail, "error", map[string]interface{}{"error": "not found"}, start)
		return
	}
	// Apply name
	if req.Name != nil {
		if err := h.database.UpdateUserName(oldEmail, strings.TrimSpace(*req.Name)); err != nil {
			http.Error(w, "failed to update name", http.StatusInternalServerError)
			h.auditUserAction(r, "user.update", oldEmail, "error", map[string]interface{}{"error": "name update failed"}, start)
			return
		}
	}
	newEmail := oldEmail
	// Apply email change
	if req.Email != nil {
		candidate := strings.TrimSpace(*req.Email)
		if !isValidEmail(candidate) {
			http.Error(w, "invalid email format", http.StatusBadRequest)
			return
		}
		// If the candidate email is the same as the current (case-insensitive), skip update
		if !strings.EqualFold(candidate, oldEmail) {
			// Conflict check only when changing to a different email
			if u, _ := h.rbacManager.GetUser(candidate); u != nil {
				http.Error(w, "user already exists", http.StatusConflict)
				return
			}
			if err := h.database.UpdateUserEmail(oldEmail, candidate); err != nil {
				http.Error(w, "failed to update email", http.StatusInternalServerError)
				h.auditUserAction(r, "user.update", oldEmail, "error", map[string]interface{}{"error": "email update failed"}, start)
				return
			}
			newEmail = candidate
			// Ensure RBAC enforcer has grouping for the new email
			_ = h.rbacManager.SaveUser(newEmail, &rbac.UserConfig{Name: dbUser.Name, Roles: dbUser.Roles})
		}
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	// Resolve ID for resource; include email in details
	userWithID, _ := h.rbacManager.GetUserWithID(newEmail)
	res := newEmail
	if userWithID != nil && userWithID.ID > 0 {
		res = fmt.Sprintf("%d", userWithID.ID)
	}
	details := map[string]interface{}{"email": newEmail}
	if req.Name != nil {
		details["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Email != nil {
		details["prev_email"] = oldEmail
	}
	h.auditUserAction(r, "user.update", res, "success", details, start)
}

// ServeStatic serves the React app's static files
func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	// In dev, proxy to Vite dev server if configured
	if h.devProxy != nil {
		h.devProxy.ServeHTTP(w, r)
		return
	}
	// Serve the built SPA from web/dist under the /.gateway/web/ path
	staticDir := "web/dist"
	if _, err := os.Stat(staticDir); err != nil {
		http.NotFound(w, r)
		return
	}

	// Strip the "/.gateway/web/" prefix since the files live directly in dist
	reqPath := strings.TrimPrefix(r.URL.Path, "/.gateway/web/")
	if reqPath == "" || reqPath == "/" {
		reqPath = "index.html"
	}

	// Resolve the requested file path
	fullPath := filepath.Join(staticDir, reqPath)
	if fi, err := os.Stat(fullPath); err != nil || fi.IsDir() {
		// SPA fallback to index.html for client-side routes
		fullPath = filepath.Join(staticDir, "index.html")
	}

	http.ServeFile(w, r, fullPath)
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
		if role == "admin" || role == "ops" || role == "deployer" || role == "viewer" {
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
