package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	emailtemplates "github.com/DocSpring/convox-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/convox-gateway/internal/gateway/envutil"
	"github.com/DocSpring/convox-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/convox-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/routematch"
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
	rackAlias        string
	rackCertManager  *rackcert.Manager
}

type deployApprovalTracker struct {
	request   *db.DeployApprovalRequest
	tokenID   int64
	app       string
	releaseID string
}

type deployApprovalContextKeyType string

const deployApprovalContextKey deployApprovalContextKeyType = "deployApproval"

const maskedSecret = envutil.MaskedSecret

func clientIPFromRequest(r *http.Request) string {
	ip := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

type logAccumulator struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newLogAccumulator(limit int) *logAccumulator {
	return &logAccumulator{limit: limit}
}

func (l *logAccumulator) Write(p []byte) (int, error) {
	if l.limit <= 0 {
		return l.buf.Write(p)
	}
	remaining := l.limit - l.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		if _, err := l.buf.Write(p[:remaining]); err != nil {
			return 0, err
		}
	}
	if len(p) > remaining {
		l.truncated = true
	}
	return len(p), nil
}

func (l *logAccumulator) Bytes() []byte {
	if !l.truncated {
		return l.buf.Bytes()
	}
	out := append([]byte{}, l.buf.Bytes()...)
	out = append(out, []byte("…(truncated)")...)
	return out
}

// logAudit is a helper to log audit entries and mark the request context
func (h *Handler) logAudit(r *http.Request, al *db.AuditLog) error {
	err := audit.LogDB(h.database, al)
	if err == nil && r != nil {
		ctx := audit.MarkAuditLogCreated(r.Context())
		*r = *r.WithContext(ctx)
	}
	return err
}

func NewHandler(cfg *config.Config, rbacManager rbac.RBACManager, auditLogger *audit.Logger, database *db.Database, mailer email.Sender, rackName, rackAlias string, rackCertManager *rackcert.Manager) *Handler {
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
		rackAlias:        strings.TrimSpace(rackAlias),
		rackCertManager:  rackCertManager,
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

func (h *Handler) rackTLSConfig(ctx context.Context) (*tls.Config, error) {
	if h.rackCertManager == nil {
		return nil, nil
	}
	return h.rackCertManager.TLSConfig(ctx)
}

func (h *Handler) httpClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	tlsCfg, err := h.rackTLSConfig(ctx)
	if err != nil {
		return nil, err
	}
	return httpclient.NewRackClient(timeout, tlsCfg), nil
}

func logRackTLSMismatch(scope string, err *rackcert.FingerprintMismatchError) {
	if err == nil {
		return
	}
	log.Printf(`{"level":"error","event":"rack_tls_verification_failed","scope":"%s","expected_fingerprint":"%s","actual_fingerprint":"%s"}`, scope, err.Expected, err.Actual)
}

func (h *Handler) rackDisplay() string {
	if alias := strings.TrimSpace(h.rackAlias); alias != "" {
		return alias
	}
	return h.rackName
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
	if !routematch.IsAllowed(methodForAllow, path) {
		// Return 404 without writing an audit DB entry for non-Convox noise (e.g., .well-known, favicon, etc.)
		http.NotFound(w, r)
		return
	}

	// Check permissions (different logic for JWT vs API tokens)
	var (
		allowed         bool
		approvalTracker *deployApprovalTracker
		err             error
	)
	methodForRBAC := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForRBAC = "SOCKET"
	}
	resource, action, ok := routematch.Match(methodForRBAC, path)
	if !ok {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusNotFound, time.Since(start), fmt.Errorf("unknown route: %s %s", methodForRBAC, path))
		http.NotFound(w, r)
		return
	}

	if authUser.IsAPIToken {
		allowed, approvalTracker, err = h.evaluateAPITokenPermission(r, authUser, rackConfig, resource, action)
		if err != nil {
			if appErr, ok := err.(*deployApprovalError); ok {
				h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", appErr.status, time.Since(start), errors.New(appErr.message))
				http.Error(w, appErr.message, appErr.status)
				return
			}
			h.handleError(w, r, "failed to validate deploy approval", http.StatusInternalServerError, rackConfig.Name, start)
			return
		}
	} else {
		allowed, err = h.rbacManager.Enforce(authUser.Email, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if approvalTracker != nil {
		ctx := context.WithValue(r.Context(), deployApprovalContextKey, approvalTracker)
		r = r.WithContext(ctx)
	}

	// Additional RBAC for release/environment set operations and body rewrite
	var envDiffs []envutil.EnvDiff
	if allowed && r.Method == http.MethodPost && strings.Contains(path, "/releases") {
		ok, diffs, err := h.prepareReleaseCreate(r, rackConfig, authUser.Email)
		if err != nil {
			if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
				logRackTLSMismatch("env_fetch", fpErr)
				h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackConfig.Name, start)
				return
			}
			h.handleError(w, r, err.Error(), http.StatusBadRequest, rackConfig.Name, start)
			return
		}
		envDiffs = diffs
		if !ok {
			// Deny without emitting an additional high-level releases.create deny;
			// per-key env/secrets denies were already logged in prepareReleaseCreate.
			http.Error(w, forbiddenMessage(resource, action), http.StatusForbidden)
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
		http.Error(w, forbiddenMessage(resource, action), http.StatusForbidden)
		return
	}

	// Additional gating for process exec and terminate (approval-based permissions)
	if resource == "process" && action == "exec" {
		if ok, message := h.checkProcessExec(r, authUser, path, approvalTracker); !ok {
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("process exec denied: %s", message))
			http.Error(w, message, http.StatusForbidden)
			return
		}
	}
	if resource == "process" && action == "terminate" {
		if ok, message := h.checkProcessTerminate(r, authUser, path); !ok {
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("process terminate denied: %s", message))
			http.Error(w, message, http.StatusForbidden)
			return
		}
	}

	// Block destructive actions when not allowed by settings
	if !h.allowDestructive {
		if isDestructive(methodForRBAC, resource, action) {
			// Log as denied (RBAC) for consistency
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("destructive actions are disabled by policy"))
			http.Error(w, "Destructive rack actions are disabled by policy", http.StatusForbidden)
			return
		}
	}

	// Pre-validate audit log requirements BEFORE proxying to ensure we can return proper error
	if !audit.HasAuditLogBeenCreated(r.Context()) {
		action, resource := h.auditLogger.ParseConvoxAction(r.URL.Path, r.Method)
		if action == "unknown" || resource == "unknown" {
			errorMsg := fmt.Sprintf("cannot determine action/resource for %s %s", r.Method, r.URL.Path)
			log.Printf(`{"level":"error","error":"audit_failure","message":"%s","method":"%s","path":"%s","action":"%s","resource":"%s"}`, errorMsg, r.Method, r.URL.Path, action, resource)
			h.handleError(w, r, errorMsg, http.StatusInternalServerError, rackConfig.Name, start)
			return
		}
		resourceType := h.auditLogger.InferResourceType(r.URL.Path, action)
		if resourceType == "unknown" {
			errorMsg := fmt.Sprintf("cannot determine resource type for %s %s", r.Method, r.URL.Path)
			log.Printf(`{"level":"error","error":"audit_failure","message":"%s","method":"%s","path":"%s","action":"%s","resource":"%s","resource_type":"%s"}`, errorMsg, r.Method, r.URL.Path, action, resource, resourceType)
			h.handleError(w, r, errorMsg, http.StatusInternalServerError, rackConfig.Name, start)
			return
		}
	}

	// Pre-capture system parameters if this is a rack params update
	var beforeParams map[string]string
	isRackParamsUpdate := (r.Method == http.MethodPut && routematch.KeyMatch3(path, "/system"))
	if isRackParamsUpdate {
		if params, err := h.fetchSystemParams(r.Context(), rackConfig); err == nil {
			beforeParams = params
		}
	}

	// Forward the request to the rack
	status, err := h.forwardRequest(w, r, rackConfig, path, authUser.Email)
	if tracker := getDeployApprovalTracker(r.Context()); tracker != nil && err == nil && status >= 200 && status < 300 {
		// Mark the approval as promoted/consumed on successful release promotion
		h.markDeployApprovalPromoted(tracker)
	}
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("proxy_forward", fpErr)
			h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackConfig.Name, start)
			return
		}
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rackConfig.Name, start)
		return
	}

	if status == 0 {
		status = http.StatusOK
	}

	// Create generic audit log if no explicit audit logs were created during request handling
	// (validation already happened before proxy, so action/resource/resourceType are guaranteed to be valid)
	if !audit.HasAuditLogBeenCreated(r.Context()) {
		action, resource := h.auditLogger.ParseConvoxAction(r.URL.Path, r.Method)
		resourceType := h.auditLogger.InferResourceType(r.URL.Path, action)

		var tokenIDPtr *int64
		if tokenIDHeader := strings.TrimSpace(r.Header.Get("X-API-Token-ID")); tokenIDHeader != "" {
			if parsed, parseErr := strconv.ParseInt(tokenIDHeader, 10, 64); parseErr == nil {
				tokenIDPtr = &parsed
			}
		}

		auditLog := &db.AuditLog{
			UserEmail:      authUser.Email,
			UserName:       r.Header.Get("X-User-Name"),
			APITokenID:     tokenIDPtr,
			APITokenName:   strings.TrimSpace(r.Header.Get("X-API-Token-Name")),
			ActionType:     "convox",
			Action:         action,
			Resource:       resource,
			ResourceType:   resourceType,
			Details:        h.auditLogger.BuildDetailsJSON(r),
			IPAddress:      h.auditLogger.GetClientIP(r),
			UserAgent:      r.UserAgent(),
			Status:         h.auditLogger.MapHttpStatusToStatus(status),
			RBACDecision:   "allow",
			HTTPStatus:     status,
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
			EventCount:     1,
		}
		if dbErr := h.logAudit(r, auditLog); dbErr != nil {
			log.Printf("Failed to store audit log in database: %v", dbErr)
		}
	}

	// Log request to stdout for CloudWatch (after audit validation passes)
	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "allow", status, time.Since(start), nil)

	// On success, write detailed audit entries for each env change
	if status >= 200 && status < 300 {
		skipManualReleaseLog := r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps/{app}/releases")
		releaseIDs := r.Header.Values("X-Release-Created")
		if len(releaseIDs) > 0 {
			for _, rel := range releaseIDs {
				rel = strings.TrimSpace(rel)
				if rel == "" {
					continue
				}
				if skipManualReleaseLog {
					continue
				}
				_ = h.logAudit(r, &db.AuditLog{
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
					IPAddress:      clientIPFromRequest(r),
					UserAgent:      r.UserAgent(),
				})
			}
		}
		h.logEnvDiffs(r, authUser.Email, rackConfig.Name, envDiffs)
		// If this was a rack params update, compute diff and notify admins + audit
		if isRackParamsUpdate {
			if afterParams, err := h.fetchSystemParams(r.Context(), rackConfig); err == nil {
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

// fetchSystemParams retrieves /system and returns its Parameters map.
func (h *Handler) fetchSystemParams(ctx context.Context, rack config.RackConfig) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	targetURL := base + "/system"
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	client, err := h.httpClient(ctx, 15*time.Second)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("fetch_system_params", fpErr)
			return nil, fpErr
		}
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup
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
	subject := fmt.Sprintf("Convox Gateway (%s): %s changed rack parameters", h.rackDisplay(), actor)
	text, html, _ := emailtemplates.RenderRackParamsChanged(h.rackDisplay(), actor, b.String())
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
	_ = h.logAudit(r, &db.AuditLog{
		UserEmail:    actor,
		UserName:     r.Header.Get("X-User-Name"),
		ActionType:   "convox",
		Action:       "rack.params.set",
		ResourceType: "rack",
		Resource:     h.rackName,
		Details:      string(b),
		IPAddress:    clientIPFromRequest(r),
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

// prepareReleaseCreate parses POST body env data, merges masked values from latest release,
// enforces RBAC (environment:set and secrets:set), rewrites the request body with the merged env,
// and returns a list of diffs for auditing.
func (h *Handler) prepareReleaseCreate(r *http.Request, rack config.RackConfig, email string) (bool, []envutil.EnvDiff, error) {
	// Read and buffer original body
	var bodyBuf []byte
	if r.Body != nil {
		var err error
		bodyBuf, err = io.ReadAll(r.Body)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		if err := r.Body.Close(); err != nil {
			return false, nil, fmt.Errorf("failed to close request body: %w", err)
		}
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
				_ = h.logAudit(r, &db.AuditLog{
					UserEmail:      email,
					UserName:       userName,
					ActionType:     "convox",
					Action:         "secrets.set",
					ResourceType:   "secret",
					Resource:       fmt.Sprintf("%s/%s", app, key),
					Details:        "{}",
					IPAddress:      clientIPFromRequest(r),
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
			_ = h.logAudit(r, &db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, k),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      clientIPFromRequest(r),
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
	tlsCfg, err := h.rackTLSConfig(r.Context())
	if err != nil {
		return false, nil, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}
	baseEnv, err := envutil.FetchLatestEnvMap(rack, app, tlsCfg)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("env_fetch", fpErr)
			return false, nil, fpErr
		}
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
			_ = h.logAudit(r, &db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, key),
				Details:        "{}",
				IPAddress:      clientIPFromRequest(r),
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
	diffs := make([]envutil.EnvDiff, 0)
	removed := make(map[string]envutil.EnvDiff)
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
			diffs = append(diffs, envutil.EnvDiff{Key: key, OldVal: base, NewVal: val, Secret: isSecret})
		}
	}
	for key, base := range baseEnv {
		if _, ok := posted[key]; ok {
			continue
		}
		removed[key] = envutil.EnvDiff{Key: key, OldVal: base, NewVal: "", Secret: h.isSecretKey(key)}
	}
	if len(removed) > 0 {
		for _, diff := range removed {
			diffs = append(diffs, diff)
		}
	}

	// Deny any modifications to protected env vars
	for _, d := range diffs {
		if h.isProtectedKey(d.Key) {
			// Log denied change for protected key
			userName := r.Header.Get("X-User-Name")
			app := extractAppFromPath(r.URL.Path)
			_ = h.logAudit(r, &db.AuditLog{
				UserEmail:      email,
				UserName:       userName,
				ActionType:     "convox",
				Action:         "env.set",
				ResourceType:   "env",
				Resource:       fmt.Sprintf("%s/%s", app, d.Key),
				Details:        "{\"error\":\"protected key change denied\"}",
				IPAddress:      clientIPFromRequest(r),
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
		if _, removed := removed[k]; removed {
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

func extractAppFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// expect apps/{app}/releases
	if len(parts) >= 3 && parts[0] == "apps" && parts[2] == "releases" {
		return parts[1]
	}
	return ""
}

// (removed unused helpers fetchLatestEnvMap and parseEnvString)

func (h *Handler) logEnvDiffs(r *http.Request, email, rack string, diffs []envutil.EnvDiff) {
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
		if strings.TrimSpace(d.NewVal) == "" {
			if d.Secret {
				action = "secrets.unset"
			} else {
				action = "env.unset"
			}
		}
		_ = h.logAudit(r, &db.AuditLog{
			UserEmail:      email,
			UserName:       userName,
			ActionType:     "convox",
			Action:         action,
			ResourceType:   rtype,
			Resource:       fmt.Sprintf("%s/%s", app, d.Key),
			Details:        details,
			IPAddress:      clientIPFromRequest(r),
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
	original := path
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
		if err := r.Body.Close(); err != nil {
			return 0, fmt.Errorf("failed to close request body: %w", err)
		}
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

	client, err := h.httpClient(r.Context(), 30*time.Second)
	if err != nil {
		log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
		return 0, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			return 0, fpErr
		}
		return 0, fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response cleanup

	// Read full response body (so we can optionally log it and/or filter) then send to client
	// Decide whether we need to buffer the response (only for JSON we mutate or inspect)
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isJSON := strings.Contains(ct, "application/json")
	pth := original
	filterRelease := isJSON && (routematch.KeyMatch3(pth, "/apps/{app}/releases") || routematch.KeyMatch3(pth, "/apps/{app}/releases/{id}"))
	shouldCapture := false
	if isJSON {
		switch r.Method {
		case http.MethodPost:
			shouldCapture = true
		case http.MethodGet:
			if routematch.KeyMatch3(pth, "/apps/{app}/builds/{id}") || routematch.KeyMatch3(pth, "/apps/{app}/releases/{id}") {
				shouldCapture = true
			}
		}
	}
	needsBuffer := filterRelease || shouldCapture

	var body []byte
	var respReader io.Reader
	var bytesWritten int64
	var logSnippet []byte
	if needsBuffer {
		var err error
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read response body: %w", err)
		}
		if filterRelease {
			body = h.filterReleaseEnvForUser(userEmail, body, false)
		}
		if shouldCapture {
			h.captureResourceCreator(r, pth, body, userEmail)
		}
		respReader = bytes.NewReader(body)
	} else {
		respReader = resp.Body
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

	if needsBuffer {
		var err error
		bytesWritten, err = io.Copy(w, respReader)
		if err != nil {
			return resp.StatusCode, fmt.Errorf("failed to write response body: %w", err)
		}
		if h.config.LogResponseBodies {
			max := h.config.LogResponseMaxBytes
			logBody := body
			if max > 0 && len(logBody) > max {
				logBody = append([]byte{}, logBody[:max]...)
				logBody = append(logBody, []byte("…(truncated)")...)
			}
			logSnippet = logBody
		}
	} else {
		if h.config.LogResponseBodies {
			acc := newLogAccumulator(h.config.LogResponseMaxBytes)
			reader := io.TeeReader(respReader, acc)
			var err error
			bytesWritten, err = io.Copy(w, reader)
			if err != nil {
				return resp.StatusCode, fmt.Errorf("failed to stream response body: %w", err)
			}
			logSnippet = acc.Bytes()
		} else {
			var err error
			bytesWritten, err = io.Copy(w, respReader)
			if err != nil {
				return resp.StatusCode, fmt.Errorf("failed to stream response body: %w", err)
			}
		}
	}

	// Optional response logging
	if h.config.LogResponseBodies {
		ctHeader := resp.Header.Get("Content-Type")
		upstreamMethod := ""
		upstreamURL := ""
		if resp.Request != nil {
			upstreamMethod = resp.Request.Method
			if resp.Request.URL != nil {
				upstreamURL = resp.Request.URL.String()
			}
		}
		fmt.Printf("DEBUG RESPONSE %s %s -> %d ct=%q len=%d upstream_method=%s upstream_url=%q body=%s\n",
			r.Method, path, resp.StatusCode, ctHeader, bytesWritten, upstreamMethod, upstreamURL, string(logSnippet))
	}

	return resp.StatusCode, nil
}

// recordResourceCreator stores the user->resource mapping if possible
func (h *Handler) recordResourceCreator(resourceType, resourceID, email string) bool {
	if h.database == nil || h.rbacManager == nil {
		return false
	}
	u, err := h.rbacManager.GetUserWithID(email)
	if err != nil || u == nil {
		return false
	}
	created, err := h.database.CreateUserResource(u.ID, resourceType, resourceID)
	if err != nil {
		return false
	}
	return created
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

	setResource := func(resourceType, resourceID string, setAudit bool) bool {
		if strings.TrimSpace(resourceID) == "" {
			return false
		}
		created := h.recordResourceCreator(resourceType, resourceID, email)
		if setAudit && created {
			r.Header.Set("X-Audit-Resource", resourceID)
		}
		return created
	}

	obj, ok := payload.(map[string]interface{})
	if !ok {
		return
	}

	if r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps") {
		if name := extractJSONString(obj["name"]); name != "" {
			setResource("app", name, true)
		}
	}

	if r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps/{app}/builds") {
		if id := extractJSONString(obj["id"]); id != "" {
			setResource("build", id, true)
		}
		if rel := extractJSONString(obj["release"]); rel != "" {
			if h.recordResourceCreator("release", rel, email) {
				r.Header.Add("X-Release-Created", rel)
			}
		}
	}
	if r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps/{app}/objects/tmp/{name}") {
		key := extractJSONString(obj["key"])
		if key == "" {
			key = extractJSONString(obj["id"])
		}
		if key == "" {
			segments := strings.Split(strings.TrimSpace(path), "/")
			if len(segments) > 0 {
				key = segments[len(segments)-1]
			}
		}
		if key != "" {
			setResource("object", key, false)
		}
	}

	if routematch.KeyMatch3(path, "/apps/{app}/builds/{id}") {
		if id := extractJSONString(obj["id"]); id != "" {
			h.recordResourceCreator("build", id, email)
		}
		if rel := extractJSONString(obj["release"]); rel != "" {
			if h.recordResourceCreator("release", rel, email) {
				r.Header.Add("X-Release-Created", rel)
			}
		}
	}

	if r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps/{app}/releases") {
		if id := extractJSONString(obj["id"]); id != "" {
			r.Header.Set("X-Audit-Resource", id)
			if h.recordResourceCreator("release", id, email) {
				r.Header.Add("X-Release-Created", id)
			}
		}
	}

	// Track process creation
	if r.Method == http.MethodPost && routematch.KeyMatch3(path, "/apps/{app}/services/{service}/processes") {
		if id := extractJSONString(obj["id"]); id != "" {
			h.trackProcessCreation(r, path, id, email)
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
	canEnvView, _ := h.rbacManager.Enforce(email, "env", "read")
	// Note: For native release responses, ALWAYS mask secrets regardless of secrets:read.

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

	// Env read allowed; redact secrets (always, regardless of secrets:read)
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
	if resource == "process" && (action == "terminate" || action == "stop") {
		return false
	}
	if strings.EqualFold(method, http.MethodDelete) {
		return true
	}
	// known destructive mappings
	if resource == "app" && action == "delete" {
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
	if strings.HasPrefix(strings.ToLower(rack.URL), "https://") {
		cfg, err := h.rackTLSConfig(r.Context())
		if err != nil {
			return 0, fmt.Errorf("failed to prepare rack TLS: %w", err)
		}
		if cfg != nil {
			d.TLSClientConfig = cfg
		} else {
			d.TLSClientConfig = httpclient.NewRackTLSConfig()
		}
	}

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
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("websocket_dial", fpErr)
			return 0, fpErr
		}
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
	defer upstreamConn.Close() //nolint:errcheck // websocket cleanup

	// Determine upstream-selected subprotocol (if any)
	selectedSP := ""
	if upstreamConn != nil {
		selectedSP = upstreamConn.Subprotocol()
	}

	// Upgrade client connection
	upgrader := websocket.Upgrader{
		CheckOrigin: h.checkWebSocketOrigin,
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
	defer clientConn.Close() //nolint:errcheck // websocket cleanup

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
	res, act, ok := routematch.Match(method, path)
	if !ok {
		return "", ""
	}
	return res, act
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

func forbiddenMessage(resource, action string) string {
	switch resource {
	case "secrets":
		switch action {
		case "view":
			return "You don't have permission to view secrets."
		case "set", "unset":
			return "You don't have permission to modify secrets."
		}
	case "env":
		if action == "view" {
			return "You don't have permission to view environment variables."
		}
	case "process":
		switch action {
		case "start", "run", "exec":
			return "You don't have permission to run processes."
		case "terminate", "stop":
			return "You don't have permission to stop processes."
		}
	case "release":
		switch action {
		case "create", "promote":
			return "You don't have permission to deploy releases."
		}
	}
	return "permission denied"
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

type deployApprovalError struct {
	status  int
	message string
}

func (e *deployApprovalError) Error() string { return e.message }

func tokenHasPermission(perms []string, target string) bool {
	for _, perm := range perms {
		if perm == target {
			return true
		}
	}
	return false
}

func (h *Handler) evaluateAPITokenPermission(r *http.Request, authUser *auth.AuthUser, rack config.RackConfig, resource, action string) (bool, *deployApprovalTracker, error) {
	if authUser == nil || authUser.TokenID == nil {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: forbiddenMessage(resource, action)}
	}

	// Check if token has direct permission (no approval required)
	if h.hasAPITokenPermission(authUser, resource, action) {
		return true, nil, nil
	}

	// Only release:promote requires approval in the new design
	if resource != "release" || action != "promote" {
		return false, nil, nil
	}

	// Check if token has promote-with-approval permission
	withApprovalPerm := fmt.Sprintf("convox:%s:%s-with-approval", resource, action)
	if !tokenHasPermission(authUser.Permissions, withApprovalPerm) {
		return false, nil, nil
	}

	// If deploy approvals are disabled, allow the action
	if h.config != nil && h.config.DeployApprovalsDisabled {
		return true, nil, nil
	}

	if h.database == nil {
		return false, nil, fmt.Errorf("database unavailable for deploy approvals")
	}

	// Extract app from URL path (e.g., /apps/{app}/releases/RXXX/promote)
	app := extractAppFromPath(r.URL.Path)
	if app == "" {
		return false, nil, &deployApprovalError{status: http.StatusBadRequest, message: "app not found in request"}
	}

	// Extract release ID from URL path (e.g., /apps/{app}/releases/RXXX/promote)
	releaseID := extractReleaseIDFromPath(r.URL.Path)
	if releaseID == "" {
		return false, nil, &deployApprovalError{status: http.StatusBadRequest, message: "release_id not found in request"}
	}

	// Check if an active approval exists for this (app, token, release) triple
	req, err := h.database.ActiveDeployApprovalRequestByTokenAndRelease(*authUser.TokenID, app, releaseID)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			return false, nil, &deployApprovalError{status: http.StatusForbidden, message: fmt.Sprintf("deployment approval required for release %s", releaseID)}
		}
		return false, nil, err
	}

	if req == nil {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: fmt.Sprintf("deployment approval required for release %s", releaseID)}
	}

	// Check if approval is expired
	if req.ApprovalExpiresAt != nil && time.Now().After(*req.ApprovalExpiresAt) {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: "deployment approval expired"}
	}

	// Check if approval is in approved status
	if req.Status != db.DeployApprovalRequestStatusApproved {
		return false, nil, &deployApprovalError{status: http.StatusForbidden, message: fmt.Sprintf("deployment approval status is %s (must be approved)", req.Status)}
	}

	tracker := &deployApprovalTracker{
		request:   req,
		tokenID:   *authUser.TokenID,
		app:       app,
		releaseID: releaseID,
	}

	return true, tracker, nil
}

func getDeployApprovalTracker(ctx context.Context) *deployApprovalTracker {
	if ctx == nil {
		return nil
	}
	val := ctx.Value(deployApprovalContextKey)
	if tracker, ok := val.(*deployApprovalTracker); ok {
		return tracker
	}
	return nil
}

func (h *Handler) markDeployApprovalPromoted(tracker *deployApprovalTracker) {
	if tracker == nil || h.database == nil || tracker.request == nil {
		return
	}
	if err := h.database.MarkDeployApprovalRequestPromoted(tracker.request.ID, tracker.app, tracker.releaseID, tracker.tokenID, time.Now()); err != nil {
		log.Printf("deploy approval promote update failed: %v", err)
	}
}

// checkWebSocketOrigin validates the origin header for WebSocket connections
func (h *Handler) checkWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No origin header - allow for non-browser clients (CLI tools)
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		// Invalid origin URL - reject
		return false
	}

	// Allow same-origin requests
	if r.Host == originURL.Host {
		return true
	}

	// In development mode, be more permissive
	if os.Getenv("DEV_MODE") == "true" {
		// Allow localhost origins in dev
		if strings.HasPrefix(originURL.Host, "localhost:") || originURL.Host == "localhost" {
			return true
		}
		// Allow the configured web dev server
		if webDevURL := os.Getenv("WEB_DEV_SERVER_URL"); webDevURL != "" {
			if devURL, err := url.Parse(webDevURL); err == nil {
				if originURL.Host == devURL.Host {
					return true
				}
			}
		}
	}

	// Allow configured domain
	if h.config.Domain != "" {
		// Check if origin matches the configured domain
		allowedHost := h.config.Domain
		if !strings.Contains(allowedHost, ":") && originURL.Scheme == "https" {
			allowedHost = h.config.Domain + ":443"
		} else if !strings.Contains(allowedHost, ":") && originURL.Scheme == "http" {
			allowedHost = h.config.Domain + ":80"
		}

		// Compare without default ports
		originHost := originURL.Host
		if (originURL.Scheme == "https" && strings.HasSuffix(originHost, ":443")) ||
			(originURL.Scheme == "http" && strings.HasSuffix(originHost, ":80")) {
			originHost = strings.Split(originHost, ":")[0]
		}
		if (h.config.Domain == originHost) || (allowedHost == originURL.Host) {
			return true
		}
	}

	// Reject all other origins
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
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		log.Printf("proxy: failed to encode error response: %v", err)
	}
}

// trackProcessCreation records a process created via the gateway.
func (h *Handler) trackProcessCreation(r *http.Request, path, processID, email string) {
	if h.database == nil {
		return
	}

	// Extract app name from path: /apps/{app}/services/{service}/processes
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return
	}
	app := parts[1]

	// Get creator info
	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		return
	}

	var userID, tokenID *int64
	if authUser.IsAPIToken {
		tokenID = authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(email); err == nil && user != nil {
			userID = &user.ID
		}
	}

	// Create process record (no release ID or command yet)
	if err := h.database.CreateProcess(processID, app, "", userID, tokenID); err != nil {
		log.Printf(`{"level":"error","event":"process_tracking_failed","process_id":%q,"error":%q}`, processID, err.Error())
	}
}

// checkProcessExec gates process exec with command allowlist and approval checks.
// Returns (allowed, error message).
func (h *Handler) checkProcessExec(r *http.Request, authUser *auth.AuthUser, path string, approvalTracker *deployApprovalTracker) (bool, string) {
	if h.database == nil {
		return true, "" // No database, no gating
	}

	// Extract process ID from path: /apps/{app}/processes/{pid}/exec
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return false, "invalid process path"
	}
	processID := parts[3]

	// Get command from header (this is where Convox passes the exec command)
	command := strings.TrimSpace(r.Header.Get("Command"))
	if command == "" {
		return false, "no command specified"
	}

	// Check if process exists and was created by this user/token
	process, err := h.database.GetProcess(processID)
	if err != nil {
		log.Printf(`{"level":"error","event":"process_lookup_failed","process_id":%q,"error":%q}`, processID, err.Error())
		return false, "failed to verify process ownership"
	}
	if process == nil {
		// Process not tracked - allow for regular users but deny for API tokens with -with-approval permission
		// (This handles processes created outside the gateway, like existing app processes)
		if authUser.IsAPIToken {
			// Check if they have the gated permission (exec-with-approval)
			if allowed, _ := h.rbacManager.Enforce(authUser.Email, "process", "exec-with-approval"); allowed {
				return false, "cannot exec into untracked processes (not created via gateway)"
			}
		}
		// Regular users or tokens without -with-approval can exec into any process
		return true, ""
	}

	// Verify ownership
	var isOwner bool
	if authUser.IsAPIToken && authUser.TokenID != nil {
		isOwner = process.CreatedByAPITokenID != nil && *process.CreatedByAPITokenID == *authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && user != nil {
			isOwner = process.CreatedByUserID != nil && *process.CreatedByUserID == user.ID
		}
	}

	if !isOwner {
		return false, "can only exec into processes you created"
	}

	// Check command against allowlist
	approvedCommands, err := h.database.GetApprovedCommands()
	if err != nil {
		log.Printf(`{"level":"error","event":"approved_commands_lookup_failed","error":%q}`, err.Error())
		return false, "failed to check command allowlist"
	}

	commandAllowed := false
	for _, approved := range approvedCommands {
		if command == approved {
			commandAllowed = true
			break
		}
	}

	if !commandAllowed {
		return false, fmt.Sprintf("command %q not in approved commands list", command)
	}

	// Check deploy approval if required
	if approvalTracker == nil {
		return false, "exec requires an approved deploy approval request"
	}

	// Update process with command and approval request ID
	if err := h.database.UpdateProcessCommand(processID, command, &approvalTracker.request.ID); err != nil {
		log.Printf(`{"level":"error","event":"process_command_update_failed","process_id":%q,"error":%q}`, processID, err.Error())
		// Don't fail the request if we can't update the tracking
	}

	return true, ""
}

// checkProcessTerminate gates process termination to only processes created by the requester.
// Returns (allowed, error message).
func (h *Handler) checkProcessTerminate(r *http.Request, authUser *auth.AuthUser, path string) (bool, string) {
	if h.database == nil {
		return true, "" // No database, no gating
	}

	// Extract process ID from path: /apps/{app}/processes/{pid}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return false, "invalid process path"
	}
	processID := parts[3]

	// Get process
	process, err := h.database.GetProcess(processID)
	if err != nil {
		log.Printf(`{"level":"error","event":"process_lookup_failed","process_id":%q,"error":%q}`, processID, err.Error())
		return false, "failed to verify process ownership"
	}
	if process == nil {
		// Process not tracked - allow for regular users but deny for API tokens with -with-approval permission
		if authUser.IsAPIToken {
			// Check if they have the gated permission (terminate-with-approval)
			if allowed, _ := h.rbacManager.Enforce(authUser.Email, "process", "terminate-with-approval"); allowed {
				return false, "cannot terminate untracked processes (not created via gateway)"
			}
		}
		// Regular users or tokens without -with-approval can terminate any process
		return true, ""
	}

	// Verify ownership
	var isOwner bool
	if authUser.IsAPIToken && authUser.TokenID != nil {
		isOwner = process.CreatedByAPITokenID != nil && *process.CreatedByAPITokenID == *authUser.TokenID
	} else {
		if user, err := h.rbacManager.GetUserWithID(authUser.Email); err == nil && user != nil {
			isOwner = process.CreatedByUserID != nil && *process.CreatedByUserID == user.ID
		}
	}

	if !isOwner {
		return false, "can only terminate processes you created"
	}

	// Mark process as terminated
	if err := h.database.MarkProcessTerminated(processID); err != nil {
		log.Printf(`{"level":"error","event":"process_termination_tracking_failed","process_id":%q,"error":%q}`, processID, err.Error())
		// Don't fail the request if we can't update the tracking
	}

	return true, ""
}
