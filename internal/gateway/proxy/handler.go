package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routematch"
	"github.com/getsentry/sentry-go"
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
	mfaService       *mfa.Service
	sessionManager   *auth.SessionManager
}

const maskedSecret = envutil.MaskedSecret

func NewHandler(cfg *config.Config, rbacManager rbac.RBACManager, auditLogger *audit.Logger, database *db.Database, mailer email.Sender, rackName, rackAlias string, rackCertManager *rackcert.Manager, mfaService *mfa.Service, sessionManager *auth.SessionManager) *Handler {
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
		mfaService:       mfaService,
		sessionManager:   sessionManager,
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

func (h *Handler) SetAllowDestructive(v bool) { h.allowDestructive = v }

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

	// Optional early request logging (before RBAC) for debugging
	if h.config.LogRequestHeaders || h.config.LogRequestBodies {
		var bodyBytes []byte
		if r.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err == nil {
				r.Body.Close() //nolint:errcheck
				// Restore body for downstream handlers
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		if h.config.LogRequestHeaders {
			fmt.Printf("DEBUG REQUEST HEADERS %s %s:\n", r.Method, path)
			for key, values := range r.Header {
				for _, value := range values {
					fmt.Printf("  %s: %s\n", key, value)
				}
			}
		}

		if h.config.LogRequestBodies && len(bodyBytes) > 0 {
			// Skip logging binary formats
			ct := strings.ToLower(r.Header.Get("Content-Type"))
			isBinary := strings.Contains(ct, "gzip") ||
				strings.Contains(ct, "application/octet-stream") ||
				strings.Contains(ct, "application/x-tar") ||
				strings.Contains(ct, "application/zip")

			if !isBinary {
				max := h.config.LogBodyMaxBytes
				logBody := bodyBytes
				if max > 0 && len(logBody) > max {
					logBody = append([]byte{}, logBody[:max]...)
					logBody = append(logBody, []byte("…(truncated)")...)
				}
				fmt.Printf("DEBUG REQUEST BODY %s %s len=%d body=%s\n", r.Method, path, len(bodyBytes), string(logBody))
			} else {
				fmt.Printf("DEBUG REQUEST BODY %s %s len=%d (binary, not logged)\n", r.Method, path, len(bodyBytes))
			}
		}
	}

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

	// Check permissions (different logic for session tokens vs API tokens)
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
		// For regular users (not API tokens), check direct permissions
		// API tokens use -with-approval variants that are gated by the deploy approval system
		allowed, err = h.rbacManager.Enforce(authUser.Email, rbac.ScopeConvox, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if approvalTracker != nil {
		ctx := context.WithValue(r.Context(), deployApprovalContextKey, approvalTracker)
		r = r.WithContext(ctx)
	}

	// Check if MFA is required for this route and verify if provided
	if !authUser.IsAPIToken {
		mfaErr := h.verifyMFAIfRequired(r, w, authUser, resource, action, &rackConfig, start)
		if mfaErr != nil {
			// Error already logged and response sent
			return
		}
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
	status, err := h.forwardRequest(w, r, rackConfig, path, authUser)
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
	if !audit.RequestAlreadyLogged(r) {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "allow", status, time.Since(start), nil)
	}

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
				var tokenIDPtr *int64
				if tokenIDHeader := strings.TrimSpace(r.Header.Get("X-API-Token-ID")); tokenIDHeader != "" {
					if parsed, parseErr := strconv.ParseInt(tokenIDHeader, 10, 64); parseErr == nil {
						tokenIDPtr = &parsed
					}
				}

				_ = h.logAudit(r, &db.AuditLog{
					UserEmail:      authUser.Email,
					UserName:       r.Header.Get("X-User-Name"),
					APITokenID:     tokenIDPtr,
					APITokenName:   strings.TrimSpace(r.Header.Get("X-API-Token-Name")),
					ActionType:     "convox",
					Action:         audit.BuildAction(rbac.ResourceStringRelease, rbac.ActionStringCreate),
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

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, message string, status int, rack string, start time.Time) {
	userEmail := "anonymous"
	if authUser, ok := auth.GetAuthUser(r.Context()); ok {
		userEmail = authUser.Email
	}

	// Capture 500-level errors to Sentry
	if status >= 500 && status < 600 {
		h.captureSentryError(r, fmt.Errorf("%s", message), userEmail)
	}

	if !audit.RequestAlreadyLogged(r) {
		h.auditLogger.LogRequest(r, userEmail, rack, "error", status, time.Since(start), fmt.Errorf("%s", message))
	}

	errorResponse := map[string]string{"error": message}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		log.Printf("proxy: failed to encode error response: %v", err)
	}
}

// captureSentryError captures an error to Sentry with request context and user information.
func (h *Handler) captureSentryError(r *http.Request, err error, userEmail string) {
	if err == nil {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		if r != nil {
			scope.SetRequest(r)
			scope.SetTag("http_method", r.Method)
			scope.SetTag("http_path", r.URL.Path)
		}
		if userEmail != "" && userEmail != "anonymous" {
			scope.SetUser(sentry.User{Email: userEmail})
		}
		scope.SetTag("component", "proxy")
		scope.SetTag("rack", h.rackName)
		sentry.CaptureException(err)
	})
}
