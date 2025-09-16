package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"net/url"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	emailtemplates "github.com/DocSpring/convox-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/routes"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Handler struct {
	config           *config.Config
	rbacManager      rbac.RBACManager
	auditLogger      *audit.Logger
	secretNames      map[string]struct{}
	database         *db.Database
	protectedEnv     map[string]struct{}
	allowDestructive bool
	emailer          email.Sender
	rackName         string
}

const maskedSecret = envutil.MaskedSecret

func NewHandler(cfg *config.Config, rbacManager rbac.RBACManager, auditLogger *audit.Logger, database *db.Database, mailer email.Sender, rackName string) *Handler {
	h := &Handler{
		config:           cfg,
		rbacManager:      rbacManager,
		auditLogger:      auditLogger,
		secretNames:      make(map[string]struct{}),
		database:         database,
		protectedEnv:     make(map[string]struct{}),
		allowDestructive: false,
		emailer:          mailer,
		rackName:         rackName,
	}
	// Load additional secret env var names from env (comma-separated)
	if list := strings.TrimSpace(os.Getenv("CONVOX_SECRET_ENV_VARS")); list != "" {
		for _, k := range strings.Split(list, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				h.secretNames[k] = struct{}{}
			}
		}
	}
	// Load settings from DB
	if database != nil {
		if arr, err := database.GetProtectedEnvVars(); err == nil {
			for _, k := range arr {
				h.protectedEnv[strings.ToUpper(k)] = struct{}{}
			}
		}
		if v, err := database.GetAllowDestructiveActions(); err == nil {
			h.allowDestructive = v
		}
	}
	return h
}

// SetAllowDestructive updates the in-memory destructive action toggle.
func (h *Handler) SetAllowDestructive(v bool) { h.allowDestructive = v }

// ReplaceProtectedEnv replaces the in-memory set of protected env var names.
func (h *Handler) ReplaceProtectedEnv(keys []string) {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.ToUpper(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		m[k] = struct{}{}
	}
	h.protectedEnv = m
}

// ProxyToRack handles all requests that should be proxied to the Convox rack
func (h *Handler) ProxyToRack(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Refresh dynamic settings from DB
	if h.database != nil {
		if v, err := h.database.GetAllowDestructiveActions(); err == nil {
			h.allowDestructive = v
		}
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			// rebuild map quickly (small set)
			m := make(map[string]struct{}, len(arr))
			for _, k := range arr {
				m[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
			}
			h.protectedEnv = m
		}
	}

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

	// Before any RBAC/audit, enforce an allowlist of Convox API paths.
	methodForAllow := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForAllow = "SOCKET"
	}
	if !routes.IsAllowed(methodForAllow, path) {
		// Return 404 without writing an audit DB entry for non-Convox noise (e.g., .well-known, favicon, etc.)
		http.NotFound(w, r)
		return
	}

	// Check permissions (different logic for JWT vs API tokens)
	var allowed bool
	var err error
	methodForRBAC := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForRBAC = "SOCKET"
	}
	resource, action, ok := routes.Match(methodForRBAC, path)
	if !ok {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusNotFound, time.Since(start), fmt.Errorf("unknown route: %s %s", methodForRBAC, path))
		http.NotFound(w, r)
		return
	}

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

	// Additional RBAC for release/environment set operations and body rewrite
	var envDiffs []EnvDiff
	if allowed && r.Method == http.MethodPost && strings.Contains(path, "/releases") {
		ok, diffs, err := h.prepareReleaseCreate(r, rackConfig, authUser.Email)
		if err != nil {
			h.handleError(w, r, err.Error(), http.StatusBadRequest, rackConfig.Name, start)
			return
		}
		envDiffs = diffs
		if !ok {
			// Deny without emitting an additional high-level releases.create deny;
			// per-key env/secrets denies were already logged in prepareReleaseCreate.
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
	} else if allowed && r.Method == http.MethodPost && (keyMatch3(path, "/apps/{app}/environment") || keyMatch3(path, "/apps/{app}/env")) {
		ok, diffs, err := h.prepareEnvironmentSetJSON(r, rackConfig, authUser.Email)
		if err != nil {
			h.handleError(w, r, err.Error(), http.StatusBadRequest, rackConfig.Name, start)
			return
		}
		envDiffs = diffs
		if !ok {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
	}

	if r.Method == http.MethodPost {
		if releaseID := extractReleaseIDFromPath(path); releaseID != "" {
			r.Header.Set("X-Audit-Resource", releaseID)
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	// Block destructive actions when not allowed by settings
	if !h.allowDestructive {
		if isDestructive(methodForRBAC, resource, action) {
			// Log as denied (RBAC) for consistency
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("destructive actions are disabled by policy"))
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
	}

	// Pre-capture system parameters if this is a rack params update
	var beforeParams map[string]string
	isRackParamsUpdate := (r.Method == http.MethodPut && keyMatch3(path, "/system"))
	if isRackParamsUpdate {
		if params, err := h.fetchSystemParams(rackConfig); err == nil {
			beforeParams = params
		}
	}

	// Forward the request to the rack
	status, err := h.forwardRequest(w, r, rackConfig, path, authUser.Email)
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rackConfig.Name, start)
		return
	}

	if status == 0 {
		status = http.StatusOK
	}
	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "allow", status, time.Since(start), nil)

	// On success, write detailed audit entries for each env change
	if status >= 200 && status < 300 {
		releaseIDs := r.Header.Values("X-Release-Created")
		if len(releaseIDs) > 0 {
			for _, rel := range releaseIDs {
				rel = strings.TrimSpace(rel)
				if rel == "" {
					continue
				}
				_ = h.auditLogger.LogDBEntry(&db.AuditLog{
					UserEmail:      authUser.Email,
					UserName:       r.Header.Get("X-User-Name"),
					ActionType:     "convox",
					Action:         "release.create",
					ResourceType:   "release",
					Resource:       rel,
					Status:         "success",
					RBACDecision:   "allow",
					HTTPStatus:     status,
					ResponseTimeMs: int(time.Since(start).Milliseconds()),
					IPAddress:      r.RemoteAddr,
					UserAgent:      r.UserAgent(),
				})
			}
		}
		h.logEnvDiffs(r, authUser.Email, rackConfig.Name, envDiffs)
		// If this was a rack params update, compute diff and notify admins + audit
		if isRackParamsUpdate {
			if afterParams, err := h.fetchSystemParams(rackConfig); err == nil {
				changes := diffParams(beforeParams, afterParams)
				if len(changes) > 0 {
					h.notifyRackParamsChanged(r, authUser.Email, changes)
					h.auditRackParamsChanged(r, authUser.Email, changes)
				}
			}
		}
	}
	r.Header.Del("X-Release-Created")
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

	// Enforce allowlist for rack-scoped proxy as well
	methodForAllow := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForAllow = "SOCKET"
	}
	if !routes.IsAllowed(methodForAllow, "/"+path) {
		http.NotFound(w, r)
		return
	}

	var allowed bool
	var err error
	methodForRBAC := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForRBAC = "SOCKET"
	}
	resource, action, ok := routes.Match(methodForRBAC, "/"+path)
	if !ok {
		h.auditLogger.LogRequest(r, authUser.Email, rack, "deny", http.StatusNotFound, time.Since(start), fmt.Errorf("unknown route: %s %s", methodForRBAC, path))
		http.NotFound(w, r)
		return
	}

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

	if allowed && r.Method == http.MethodPost && strings.Contains(path, "/releases") {
		if au, ok := auth.GetAuthUser(r.Context()); ok {
			ok2, diffs, err := h.prepareReleaseCreate(r, rackConfig, au.Email)
			if err != nil {
				h.handleError(w, r, err.Error(), http.StatusBadRequest, rack, start)
				return
			}
			if !ok2 {
				// Deny without duplicating high-level releases.create deny; per-key denies logged already
				http.Error(w, "permission denied", http.StatusForbidden)
				return
			}
			// success path will log diffs via outer caller (this code path is for ProxyRequest route)
			_ = diffs
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rack, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	if !h.allowDestructive {
		if isDestructive(methodForRBAC, resource, action) {
			h.auditLogger.LogRequest(r, authUser.Email, rack, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("destructive actions are disabled by policy"))
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
	}

	// Pre-capture params for PUT /system on rack-scoped proxy too
	var beforeParams map[string]string
	isRackParamsUpdate := (r.Method == http.MethodPut && keyMatch3("/"+path, "/system"))
	if isRackParamsUpdate {
		if params, err := h.fetchSystemParams(rackConfig); err == nil {
			beforeParams = params
		}
	}

	status, err := h.forwardRequest(w, r, rackConfig, path, authUser.Email)
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rack, start)
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	h.auditLogger.LogRequest(r, authUser.Email, rack, "allow", status, time.Since(start), nil)

	if status >= 200 && status < 300 {
		releaseIDs := r.Header.Values("X-Release-Created")
		if len(releaseIDs) > 0 {
			for _, rel := range releaseIDs {
				rel = strings.TrimSpace(rel)
				if rel == "" {
					continue
				}
				_ = h.auditLogger.LogDBEntry(&db.AuditLog{
					UserEmail:      authUser.Email,
					UserName:       r.Header.Get("X-User-Name"),
					ActionType:     "convox",
					Action:         "release.create",
					ResourceType:   "release",
					Resource:       rel,
					Status:         "success",
					RBACDecision:   "allow",
					HTTPStatus:     status,
					ResponseTimeMs: int(time.Since(start).Milliseconds()),
					IPAddress:      r.RemoteAddr,
					UserAgent:      r.UserAgent(),
				})
			}
		}
	}
	r.Header.Del("X-Release-Created")
	if status >= 200 && status < 300 && isRackParamsUpdate {
		if afterParams, err := h.fetchSystemParams(rackConfig); err == nil {
			changes := diffParams(beforeParams, afterParams)
			if len(changes) > 0 {
				h.notifyRackParamsChanged(r, authUser.Email, changes)
				h.auditRackParamsChanged(r, authUser.Email, changes)
			}
		}
	}
}

// fetchSystemParams retrieves /system and returns its Parameters map.
func (h *Handler) fetchSystemParams(rack config.RackConfig) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	targetURL := base + "/system"
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	client := httpclient.NewRackClient(15 * time.Second)
	// Do not follow redirects for rack system fetches
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var payload struct {
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Parameters == nil {
		payload.Parameters = map[string]string{}
	}
	// Copy
	out := make(map[string]string, len(payload.Parameters))
	for k, v := range payload.Parameters {
		out[k] = v
	}
	return out, nil
}

// paramChange represents a single parameter change
type paramChange struct{ Key, Old, New string }

func diffParams(before, after map[string]string) []paramChange {
	changes := []paramChange{}
	if after == nil {
		return changes
	}
	// include keys from both maps
	keys := map[string]struct{}{}
	for k := range after {
		keys[k] = struct{}{}
	}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range keys {
		ov := before[k]
		nv := after[k]
		if ov != nv {
			changes = append(changes, paramChange{Key: k, Old: ov, New: nv})
		}
	}
	return changes
}

// notifyRackParamsChanged emails admins about rack parameter changes.
func (h *Handler) notifyRackParamsChanged(r *http.Request, actor string, changes []paramChange) {
	if h.emailer == nil || h.rbacManager == nil || len(changes) == 0 {
		return
	}
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	// Build value string listing changes
	var b strings.Builder
	for i, c := range changes {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s -> %s", c.Key, c.Old, c.New)
	}
	subject := fmt.Sprintf("Convox Gateway (%s): %s changed rack parameters", h.rackName, actor)
	text, html, _ := emailtemplates.RenderRackParamsChanged(h.rackName, actor, b.String())
	_ = h.emailer.SendMany(admins, subject, text, html)
}

// auditRackParamsChanged writes a DB audit entry with specific change details.
func (h *Handler) auditRackParamsChanged(r *http.Request, actor string, changes []paramChange) {
	if h.database == nil || len(changes) == 0 {
		return
	}
	// Build details JSON
	payload := map[string]interface{}{"changes": func() map[string]map[string]string {
		m := map[string]map[string]string{}
		for _, c := range changes {
			m[c.Key] = map[string]string{"old": c.Old, "new": c.New}
		}
		return m
	}()}
	b, _ := json.Marshal(payload)
	_ = audit.LogDB(h.database, &db.AuditLog{
		UserEmail:    actor,
		UserName:     r.Header.Get("X-User-Name"),
		ActionType:   "convox",
		Action:       "rack.params.set",
		ResourceType: "rack",
		Resource:     h.rackName,
		Details:      string(b),
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})
}

// getAdminEmails returns emails of users with the admin role.
func (h *Handler) getAdminEmails() []string {
	if h.rbacManager == nil {
		return nil
	}
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

// checkEnvSetPermissions inspects request headers for env vars being set and enforces environment/secrets set permissions.
func (h *Handler) checkEnvSetPermissions(r *http.Request, email string) bool {
	// Extract keys from known headers
	keys := h.extractEnvKeysFromHeaders(r.Header)
	if len(keys) == 0 {
		// No explicit env changes detected; allow
		return true
	}
	// Require env:set for any env changes
	canEnvSet, _ := h.rbacManager.Enforce(email, "env", "set")
	if !canEnvSet {
		return false
	}
	// For secret keys, require secrets:set
	canSecretsSet, _ := h.rbacManager.Enforce(email, "secrets", "set")
	if !canSecretsSet {
		for _, k := range keys {
			if h.isSecretKey(k) {
				return false
			}
		}
	}
	return true
}

func (h *Handler) extractEnvKeysFromHeaders(hdr http.Header) []string {
	keys := make([]string, 0)
	for name, vals := range hdr {
		ln := strings.ToLower(name)
		if ln == "env" || ln == "environment" || ln == "release-env" {
			for _, v := range vals {
				for _, line := range strings.Split(v, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.SplitN(line, "=", 2)
					k := strings.TrimSpace(parts[0])
					if k != "" {
						keys = append(keys, k)
					}
				}
			}
		}
	}
	return keys
}

// EnvDiff represents a single env var change
type EnvDiff struct {
	Key    string
	OldVal string
	NewVal string
	Secret bool
}

// prepareReleaseCreate parses POST body env data, merges masked values from latest release,
// enforces RBAC (environment:set and secrets:set), rewrites the request body with the merged env,
// and returns a list of diffs for auditing.
func (h *Handler) prepareReleaseCreate(r *http.Request, rack config.RackConfig, email string) (bool, []EnvDiff, error) {
	// Read and buffer original body
	var bodyBuf []byte
	if r.Body != nil {
		var err error
		bodyBuf, err = io.ReadAll(r.Body)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		r.Body.Close()
	}
	// Parse form
	vals, err := url.ParseQuery(string(bodyBuf))
	if err != nil {
		return false, nil, fmt.Errorf("invalid form body: %w", err)
	}
	envStr := vals.Get("env")
	if envStr == "" {
		// no env set attempt => allow
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return true, nil, nil
	}

	// Get app name from path /apps/{app}/releases
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("could not infer app name from path")
	}

	// Parse posted env into ordered keys
	postedLines := strings.Split(envStr, "\n")
	posted := make(map[string]string)
	order := make([]string, 0, len(postedLines))
	for _, ln := range postedLines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := ""
		if len(parts) == 2 {
			val = parts[1]
		}
		if key == "" {
			continue
		}
		if _, seen := posted[key]; !seen {
			order = append(order, key)
		}
		posted[key] = val
	}

	// If attempting to set secret values without permission, deny early (no need to fetch base)
	canSecretsSet, _ := h.rbacManager.Enforce(email, "secrets", "set")
	if !canSecretsSet {
		offending := make([]string, 0)
		for _, k := range order {
			if h.isSecretKey(k) && posted[k] != maskedSecret {
				offending = append(offending, k)
			}
		}
		if len(offending) > 0 {
			// Log denied secrets.set per offending key for audit clarity
			userName := r.Header.Get("X-User-Name")
			for _, key := range offending {
				_ = h.auditLogger.LogDBEntry(&db.AuditLog{
					UserEmail:      email,
					UserName:       userName,
					ActionType:     "convox",
					Action:         "secrets.set",
					ResourceType:   "secret",
					Resource:       fmt.Sprintf("%s/%s", app, key),
					Details:        "{}",
					IPAddress:      r.RemoteAddr,
					UserAgent:      r.UserAgent(),
					Status:         "denied",
					RBACDecision:   "deny",
					HTTPStatus:     http.StatusForbidden,
					ResponseTimeMs: 0,
				})
			}
			return false, nil, nil
		}
	}

	// If posting any protected key explicitly, deny immediately (no change to protected keys allowed)
	for k := range posted {
		if h.isProtectedKey(k) {
			userName := r.Header.Get("X-User-Name")
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
	}

	// Fetch latest env map from rack (needed to fill back masked values and compute diffs)
	baseEnv, err := envutil.FetchLatestEnvMap(rack, app)
	if err != nil {
		// If fetch fails, fall back to submitted body without rewrite
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("failed to fetch latest env: %w", err)
	}

	// Permissions
	canEnvSet, _ := h.rbacManager.Enforce(email, "env", "set")
	canSecretsSet, _ = h.rbacManager.Enforce(email, "secrets", "set")
	if !canEnvSet {
		// Log denied env.set entries for submitted keys
		userName := r.Header.Get("X-User-Name")
		for _, key := range order {
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, key),
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
		}
		return false, nil, nil
	}

	// Do not require protected keys to be present in the payload; we will carry them over from base below.

	// Merge masked values and compute diffs
	merged := make(map[string]string)
	diffs := make([]EnvDiff, 0)
	for _, key := range order {
		val := posted[key]
		base := baseEnv[key]
		isSecret := h.isSecretKey(key)
		// If masked, keep base value
		if val == maskedSecret {
			merged[key] = base
			continue
		}
		// If changing a secret without permission, deny
		if isSecret && !canSecretsSet && val != base {
			return false, nil, nil
		}
		merged[key] = val
		if val != base {
			diffs = append(diffs, EnvDiff{Key: key, OldVal: base, NewVal: val, Secret: isSecret})
		}
	}

	// Deny any modifications to protected env vars
	for _, d := range diffs {
		if h.isProtectedKey(d.Key) {
			// Log denied change for protected key
			userName := r.Header.Get("X-User-Name")
			app := extractAppFromPath(r.URL.Path)
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, d.Key),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
	}

	// Recompose env string preserving submitted order and appending any base-only keys
	var b strings.Builder
	used := map[string]struct{}{}
	for i, k := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(merged[k])
		used[k] = struct{}{}
	}
	// Append remaining base keys to ensure full env for release
	for k, v := range baseEnv {
		if _, ok := used[k]; ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	vals.Set("env", b.String())
	newBody := []byte(vals.Encode())
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	// Ensure Content-Length is ignored downstream (we strip it in response), request side proxy will re-create
	r.ContentLength = int64(len(newBody))
	return true, diffs, nil
}

// prepareEnvironmentSetJSON inspects POST /apps/{app}/environment (or /env) JSON body
// to enforce env/secrets permissions and protected env vars. Returns (ok, diffs, err).
func (h *Handler) prepareEnvironmentSetJSON(r *http.Request, rack config.RackConfig, email string) (bool, []EnvDiff, error) {
	// Buffer original body
	var bodyBuf []byte
	if r.Body != nil {
		var err error
		bodyBuf, err = io.ReadAll(r.Body)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		r.Body.Close()
	}
	// Parse JSON map[string]string
	posted := map[string]string{}
	if len(bodyBuf) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(bodyBuf, &raw); err != nil {
			return false, nil, fmt.Errorf("invalid JSON body: %w", err)
		}
		for k, v := range raw {
			if v == nil {
				posted[k] = ""
				continue
			}
			switch t := v.(type) {
			case string:
				posted[k] = t
			default:
				posted[k] = fmt.Sprintf("%v", t)
			}
		}
	}

	// Determine app name from path /apps/{app}/environment
	app := ""
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "apps" && (parts[2] == "environment" || parts[2] == "env") {
		app = parts[1]
	}
	if app == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("could not infer app name from path")
	}

	// Fetch current env from rack for comparison
	baseEnv, err := envutil.FetchLatestEnvMap(rack, app)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		return false, nil, fmt.Errorf("failed to fetch latest env: %w", err)
	}

	// Early deny if posting any protected key explicitly
	for k := range posted {
		if h.isProtectedKey(k) {
			userName := r.Header.Get("X-User-Name")
			app := ""
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) >= 3 {
				app = parts[1]
			}
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
	}

	// RBAC checks
	canEnvSet, _ := h.rbacManager.Enforce(email, "env", "set")
	if !canEnvSet {
		// Log denied per posted key
		userName := r.Header.Get("X-User-Name")
		app := ""
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 {
			app = parts[1]
		}
		for k := range posted {
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
		}
		return false, nil, nil
	}
	canSecretsSet, _ := h.rbacManager.Enforce(email, "secrets", "set")

	diffs := make([]EnvDiff, 0)
	// Compare posted values against base; block secret changes without permission and any protected key changes
	for k, newVal := range posted {
		baseVal := baseEnv[k]
		isSecret := h.isSecretKey(k)
		if isSecret && !canSecretsSet && newVal != baseVal {
			// Deny secret change without permission
			userName := r.Header.Get("X-User-Name")
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "secrets.set",
				ResourceType:   "secret",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
		if h.isProtectedKey(k) && newVal != baseVal {
			// Deny changes to protected keys
			userName := r.Header.Get("X-User-Name")
			_ = h.auditLogger.LogDBEntry(&db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      r.RemoteAddr,
				UserAgent:      r.UserAgent(),
				Status:         "denied",
				RBACDecision:   "deny",
				HTTPStatus:     http.StatusForbidden,
				ResponseTimeMs: 0,
			})
			return false, nil, nil
		}
		if newVal != baseVal {
			diffs = append(diffs, EnvDiff{Key: k, OldVal: baseVal, NewVal: newVal, Secret: isSecret})
		}
	}

	// Restore body for forwarding unchanged
	r.Body = io.NopCloser(bytes.NewReader(bodyBuf))
	r.ContentLength = int64(len(bodyBuf))
	return true, diffs, nil
}

func extractAppFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// expect apps/{app}/releases
	if len(parts) >= 3 && parts[0] == "apps" && parts[2] == "releases" {
		return parts[1]
	}
	return ""
}

// (removed unused helpers fetchLatestEnvMap and parseEnvString)

func (h *Handler) logEnvDiffs(r *http.Request, email, rack string, diffs []EnvDiff) {
	if len(diffs) == 0 {
		return
	}
	userName := r.Header.Get("X-User-Name")
	app := extractAppFromPath(r.URL.Path)
	for _, d := range diffs {
		// Mask only secret values in audit details
		oldVal := d.OldVal
		newVal := d.NewVal
		if d.Secret {
			oldVal = "[REDACTED]"
			newVal = "[REDACTED]"
		}
		details := fmt.Sprintf("{\"old\":\"%s\",\"new\":\"%s\"}", escapeJSONString(oldVal), escapeJSONString(newVal))
		action := "env.set"
		rtype := "env"
		if d.Secret {
			action = "secrets.set"
			rtype = "secret"
		}
		_ = h.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:      email,
			UserName:       userName,
			ActionType:     "convox",
			Action:         action,
			ResourceType:   rtype,
			Resource:       fmt.Sprintf("%s/%s", app, d.Key),
			Details:        details,
			IPAddress:      r.RemoteAddr,
			UserAgent:      r.UserAgent(),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: 0,
		})
	}
}

// escapeJSONString minimally escapes quotes, backslashes and newlines for JSON embedding
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, rack config.RackConfig, path, userEmail string) (int, error) {
	// Build clean target URL without double slashes
	base := strings.TrimRight(rack.URL, "/")
	p := "/" + strings.TrimLeft(path, "/")
	targetURL := base + p
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Handle WebSocket upgrade requests
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		return h.proxyWebSocket(w, r, rack, targetURL, userEmail)
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read request body: %w", err)
		}
		r.Body.Close()
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to create proxy request: %w", err)
	}

	for key, values := range r.Header {
		lk := strings.ToLower(key)
		if lk == "authorization" || lk == "env" || lk == "environment" || lk == "release-env" || lk == "x-audit-resource" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Convox uses Basic Auth with configurable username (default "convox") and the API key as password
	proxyReq.Header.Set("Authorization", fmt.Sprintf("Basic %s",
		base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	proxyReq.Header.Set("X-User-Email", userEmail)
	proxyReq.Header.Set("X-Request-ID", uuid.New().String())

	// HTTP client for upstream with optional TLS overrides
	transport := httpclient.NewRackTransport()
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		// Never follow redirects so we can observe upstream responses and preserve methods
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		return 0, fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close()

	// Read full response body (so we can optionally log it and/or filter) then send to client
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	// If this is a release/environment read, filter env values before returning
	// We only process JSON payloads (Content-Type contains "application/json").
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/json") {
		// Normalize path used for RBAC routing match
		pth := path
		// Filter release payloads that include "env" string
		if keyMatch3(pth, "/apps/{app}/releases") || keyMatch3(pth, "/apps/{app}/releases/{id}") {
			body = h.filterReleaseEnvForUser(userEmail, body, false)
		}
		if r.Method == http.MethodPost {
			h.captureResourceCreator(r, pth, body, userEmail)
		}
		// Filter environment map
		if keyMatch3(pth, "/apps/{app}/environment") && r.Method == http.MethodGet {
			body = h.filterEnvironmentMapForUser(userEmail, body)
		}
	}

	// Copy headers, but drop Content-Length since we may have modified the body; let Go recalculate
	for key, values := range resp.Header {
		if strings.ToLower(key) == "content-length" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	// Optional response logging
	if h.config.LogResponseBodies {
		max := h.config.LogResponseMaxBytes
		logBody := body
		if max > 0 && len(logBody) > max {
			logBody = append([]byte{}, logBody[:max]...)
			logBody = append(logBody, []byte("…(truncated)")...)
		}
		ct := resp.Header.Get("Content-Type")
		upstreamMethod := ""
		upstreamURL := ""
		if resp.Request != nil {
			upstreamMethod = resp.Request.Method
			if resp.Request.URL != nil {
				upstreamURL = resp.Request.URL.String()
			}
		}
		fmt.Printf("DEBUG RESPONSE %s %s -> %d ct=%q len=%d upstream_method=%s upstream_url=%q body=%s\n",
			r.Method, path, resp.StatusCode, ct, len(body), upstreamMethod, upstreamURL, string(logBody))
	}

	if _, err := w.Write(body); err != nil {
		return resp.StatusCode, fmt.Errorf("failed to write response body: %w", err)
	}

	return resp.StatusCode, nil
}

// recordResourceCreator stores the user->resource mapping if possible
func (h *Handler) recordResourceCreator(resourceType, resourceID, email string) {
	if h.database == nil || h.rbacManager == nil {
		return
	}
	u, err := h.rbacManager.GetUserWithID(email)
	if err != nil || u == nil {
		return
	}
	_ = h.database.CreateUserResource(u.ID, resourceType, resourceID)
}

// captureResourceCreator persists the creator information for app/build/release create responses
// and records the resource ID for audit logging.
func (h *Handler) captureResourceCreator(r *http.Request, path string, body []byte, email string) {
	if h.database == nil || h.rbacManager == nil {
		return
	}
	if len(body) == 0 {
		return
	}

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	setResource := func(resourceType, resourceID string, setAudit bool) {
		if strings.TrimSpace(resourceID) == "" {
			return
		}
		h.recordResourceCreator(resourceType, resourceID, email)
		if setAudit {
			r.Header.Set("X-Audit-Resource", resourceID)
		}
	}

	obj, ok := payload.(map[string]interface{})
	if !ok {
		return
	}

	if r.Method == http.MethodPost && keyMatch3(path, "/apps") {
		if name := extractJSONString(obj["name"]); name != "" {
			setResource("app", name, true)
		}
	}

	if r.Method == http.MethodPost && keyMatch3(path, "/apps/{app}/builds") {
		if id := extractJSONString(obj["id"]); id != "" {
			setResource("build", id, true)
		}
		if rel := extractJSONString(obj["release"]); rel != "" {
			h.recordResourceCreator("release", rel, email)
			r.Header.Add("X-Release-Created", rel)
		}
	}

	if keyMatch3(path, "/apps/{app}/builds/{id}") {
		if id := extractJSONString(obj["id"]); id != "" {
			h.recordResourceCreator("build", id, email)
		}
		if rel := extractJSONString(obj["release"]); rel != "" {
			h.recordResourceCreator("release", rel, email)
			r.Header.Add("X-Release-Created", rel)
		}
	}

	if r.Method == http.MethodPost && keyMatch3(path, "/apps/{app}/releases") {
		if id := extractJSONString(obj["id"]); id != "" {
			h.recordResourceCreator("release", id, email)
			r.Header.Add("X-Release-Created", id)
		}
	}
}

func extractJSONString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// filterReleaseEnvForUser redacts or removes env field(s) in release JSON payloads based on RBAC permissions.
func (h *Handler) filterReleaseEnvForUser(email string, body []byte, _ bool) []byte {
	// Determine permissions
	canEnvView, _ := h.rbacManager.Enforce(email, "env", "view")
	// Note: For native release responses, ALWAYS mask secrets regardless of secrets:view.

	// If no environment view, mask all env values (do not strip, to avoid accidental clears)
	if !canEnvView {
		var any interface{}
		if err := json.Unmarshal(body, &any); err != nil {
			return body
		}
		maskAll := func(s string) string {
			lines := strings.Split(s, "\n")
			for i, ln := range lines {
				if ln == "" {
					continue
				}
				parts := strings.SplitN(ln, "=", 2)
				if len(parts) == 2 {
					parts[1] = maskedSecret
					lines[i] = parts[0] + "=" + parts[1]
				}
			}
			return strings.Join(lines, "\n")
		}
		switch v := any.(type) {
		case map[string]interface{}:
			if envv, ok := v["env"].(string); ok {
				v["env"] = maskAll(envv)
			}
			nb, _ := json.Marshal(v)
			return nb
		case []interface{}:
			for _, it := range v {
				if m, ok := it.(map[string]interface{}); ok {
					if envv, ok2 := m["env"].(string); ok2 {
						m["env"] = maskAll(envv)
					}
				}
			}
			nb, _ := json.Marshal(v)
			return nb
		default:
			return body
		}
	}

	// Env view allowed; redact secrets (always, regardless of secrets:view)
	var any interface{}
	if err := json.Unmarshal(body, &any); err != nil {
		return body
	}
	mask := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if ln == "" {
				continue
			}
			parts := strings.SplitN(ln, "=", 2)
			key := parts[0]
			if h.isSecretKey(key) && len(parts) > 1 {
				parts[1] = maskedSecret
				lines[i] = parts[0] + "=" + parts[1]
			}
		}
		return strings.Join(lines, "\n")
	}
	switch v := any.(type) {
	case map[string]interface{}:
		if envv, ok := v["env"].(string); ok {
			v["env"] = mask(envv)
		}
		nb, _ := json.Marshal(v)
		return nb
	case []interface{}:
		for _, it := range v {
			if m, ok := it.(map[string]interface{}); ok {
				if envv, ok2 := m["env"].(string); ok2 {
					m["env"] = mask(envv)
				}
			}
		}
		nb, _ := json.Marshal(v)
		return nb
	default:
		return body
	}
}

// filterEnvironmentMapForUser applies masking to the /apps/{app}/environment JSON map
// Always masks secret values regardless of secrets:view; if env:view is not permitted
// then masks all values.
func (h *Handler) filterEnvironmentMapForUser(email string, body []byte) []byte {
	canEnvView, _ := h.rbacManager.Enforce(email, "env", "view")

	var any interface{}
	if err := json.Unmarshal(body, &any); err != nil {
		return body
	}
	maskAll := func(m map[string]interface{}) {
		for k := range m {
			m[k] = maskedSecret
		}
	}
	maskSecrets := func(m map[string]interface{}) {
		for k, v := range m {
			if h.isSecretKey(k) {
				m[k] = maskedSecret
			} else {
				m[k] = v
			}
		}
	}

	switch v := any.(type) {
	case map[string]interface{}:
		if !canEnvView {
			maskAll(v)
		} else {
			maskSecrets(v)
		}
		nb, _ := json.Marshal(v)
		return nb
	default:
		return body
	}
}

func (h *Handler) isSecretKey(key string) bool {
	// Merge configured secret names and protected names (always masked)
	extra := make([]string, 0, len(h.secretNames)+len(h.protectedEnv))
	for k := range h.secretNames {
		extra = append(extra, k)
	}
	for k := range h.protectedEnv {
		extra = append(extra, k)
	}
	return envutil.IsSecretKey(key, extra)
}

func (h *Handler) isProtectedKey(key string) bool {
	_, ok := h.protectedEnv[strings.ToUpper(strings.TrimSpace(key))]
	return ok
}

// isDestructive returns true for destructive actions (delete, terminate, uninstall equivalents)
func isDestructive(method, resource, action string) bool {
	if strings.EqualFold(method, http.MethodDelete) {
		return true
	}
	// known destructive mappings
	if resource == "app" && action == "delete" {
		return true
	}
	if resource == "process" && (action == "terminate" || action == "stop") {
		return true
	}
	return false
}

// proxyWebSocket upgrades the client connection and bridges it to the rack via a WebSocket connection
func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, rack config.RackConfig, target string, userEmail string) (int, error) {
	// Prepare upstream URL (ws or wss)
	u, err := url.Parse(target)
	if err != nil {
		return 0, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	// Dial upstream websocket with Authorization header
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	header.Set("X-User-Email", userEmail)
	header.Set("X-Request-ID", uuid.New().String())
	// Some servers validate Origin during WS handshake
	header.Set("Origin", fmt.Sprintf("%s://%s", map[bool]string{true: "https", false: "http"}[strings.HasPrefix(rack.URL, "https")], u.Host))
	// Forward relevant client headers to upstream (Convox uses headers for exec options, including 'command')
	for k, vals := range r.Header {
		lk := strings.ToLower(k)
		switch lk {
		case "authorization":
			// override with rack auth
			continue
		case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions":
			continue
		case "origin":
			// we already set Origin appropriate to upstream host
			continue
		case "sec-websocket-protocol":
			// handled below
			continue
		case "x-user-email", "x-request-id":
			// already set explicitly above; avoid duplicates
			continue
		case "x-audit-resource":
			// internal auditing header; never forward upstream
			continue
		}
		for _, v := range vals {
			header.Add(k, v)
		}
	}
	// Preserve subprotocols requested by client (needed for k8s exec multiplexing)
	if sp := r.Header.Get("Sec-WebSocket-Protocol"); sp != "" {
		header.Set("Sec-WebSocket-Protocol", sp)
	}
	d := *websocket.DefaultDialer
	d.HandshakeTimeout = 10 * time.Second
	// Force HTTP/1.1 for WebSocket handshake over TLS; HTTP/2 does not support 101 Upgrade
	d.TLSClientConfig = httpclient.NewRackTLSConfig()

	// Follow up to 3 redirects for WS dial (some servers redirect mounting paths)
	var upstreamConn *websocket.Conn
	var resp *http.Response
	for i := 0; i < 3; i++ {
		upstreamConn, resp, err = d.Dial(u.String(), header)
		if err == nil {
			break
		}
		if resp != nil && (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect || resp.StatusCode == http.StatusSeeOther) {
			loc := resp.Header.Get("Location")
			if loc == "" {
				break
			}
			nu, perr := url.Parse(loc)
			if perr != nil {
				break
			}
			if !nu.IsAbs() {
				nu = u.ResolveReference(nu)
			}
			u = nu
			continue
		}
		break
	}
	if err != nil {
		if resp != nil {
			// If upstream returned a non-101 status (e.g., 404), pass it through to the client
			body, _ := io.ReadAll(resp.Body)
			for k, vs := range resp.Header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return resp.StatusCode, nil
		}
		return 0, fmt.Errorf("failed to dial upstream websocket: %w", err)
	}
	defer upstreamConn.Close()

	// Determine upstream-selected subprotocol (if any)
	selectedSP := ""
	if upstreamConn != nil {
		selectedSP = upstreamConn.Subprotocol()
	}

	// Upgrade client connection
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
		// Advertise only the upstream-selected subprotocol (pass-through) if present
		Subprotocols: func() []string {
			if selectedSP != "" {
				return []string{selectedSP}
			}
			return nil
		}(),
	}
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to upgrade client connection: %w", err)
	}
	defer clientConn.Close()

	// Bridge messages in both directions
	errc := make(chan error, 2)
	go func() {
		for {
			mt, message, err := clientConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := upstreamConn.WriteMessage(mt, message); err != nil {
				errc <- err
				return
			}
		}
	}()
	go func() {
		for {
			mt, message, err := upstreamConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := clientConn.WriteMessage(mt, message); err != nil {
				errc <- err
				return
			}
		}
	}()

	// Wait for either direction to error/close
	<-errc

	return http.StatusSwitchingProtocols, nil
}

// (removed unused helper parseSubprotocols)

// pathToResourceAction converts a path and HTTP method to resource and action for RBAC
func (h *Handler) pathToResourceAction(path, method string) (string, string) {
	res, act, ok := routes.Match(method, path)
	if !ok {
		return "", ""
	}
	return res, act
}

// keyMatch3 simplified: supports {var} placeholders and wildcards
func keyMatch3(path, pattern string) bool {
	// Convert pattern to regex
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '{' {
			for i < len(pattern) && pattern[i] != '}' {
				i++
			}
			b.WriteString("[^/]+")
			continue
		}
		if c == '*' {
			b.WriteString(".*")
			continue
		}
		if strings.ContainsRune(".+?^$()[]{}|\\", rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteString("$")
	re := b.String()
	ok, _ := regexp.MatchString(re, path)
	return ok
}

func extractReleaseIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range parts {
		if seg == "releases" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
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
