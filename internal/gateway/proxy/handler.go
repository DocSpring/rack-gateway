package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

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
	"github.com/DocSpring/rack-gateway/internal/gateway/sentryutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// Handler proxies requests through the gateway while enforcing RBAC, MFA, and
// audit logging requirements.
type Handler struct {
	config           *config.Config
	rbacManager      rbac.Manager
	auditLogger      *audit.Logger
	secretNames      map[string]struct{}
	database         *db.Database
	settingsService  *settings.Service
	protectedEnv     map[string]struct{}
	allowDestructive bool
	emailer          email.Sender
	rackName         string
	rackAlias        string
	rackCertManager  *rackcert.Manager
	mfaService       *mfa.Service
	sessionManager   *auth.SessionManager
}

// no async jobs client here; emailer may be an async sender

const maskedSecret = envutil.MaskedSecret

// NewHandler constructs a proxy handler with all required dependencies.
func NewHandler(
	cfg *config.Config,
	rbacManager rbac.Manager,
	auditLogger *audit.Logger,
	database *db.Database,
	settingsService *settings.Service,
	mailer email.Sender,
	rackName, rackAlias string,
	rackCertManager *rackcert.Manager,
	mfaService *mfa.Service,
	sessionManager *auth.SessionManager,
) *Handler {
	h := &Handler{
		config:           cfg,
		rbacManager:      rbacManager,
		auditLogger:      auditLogger,
		secretNames:      make(map[string]struct{}),
		database:         database,
		settingsService:  settingsService,
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
	// Settings are loaded on-demand from the settings service (no initialization needed)
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
	log.Printf(
		`{"level":"error","event":"rack_tls_verification_failed","scope":"%s",`+
			`"expected_fingerprint":"%s","actual_fingerprint":"%s"}`,
		scope,
		err.Expected,
		err.Actual,
	)
}

func (h *Handler) rackDisplay() string {
	if alias := strings.TrimSpace(h.rackAlias); alias != "" {
		return alias
	}
	return h.rackName
}

// SetAllowDestructive toggles whether destructive Convox operations are allowed.
func (h *Handler) SetAllowDestructive(v bool) { h.allowDestructive = v }

// ReplaceProtectedEnv replaces the set of environment keys treated as
// protected (masked in responses unless explicitly allowed).
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

// ProxyToRack handles proxying requests to the Convox rack with RBAC, MFA, and audit logging.
func (h *Handler) ProxyToRack(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	rackConfig, authUser, allowed, _, envDiffs, err := h.prepareProxyRequest(w, r, start)
	if err != nil {
		return
	}

	path := r.URL.Path
	rackPath := rbac.NormalizeRackPath(path)

	if r.Method == http.MethodPost {
		if releaseID := extractReleaseIDFromPath(rackPath); releaseID != "" {
			r.Header.Set("X-Audit-Resource", releaseID)
		}
	}

	if !allowed {
		methodForRBAC := h.determineMethod(r)
		resource, action, _ := rbac.MatchRackRoute(methodForRBAC, rackPath)
		h.auditLogger.LogRequest(
			r,
			authUser.Email,
			rackConfig.Name,
			"deny",
			http.StatusForbidden,
			time.Since(start),
			fmt.Errorf("permission denied for %s %s", r.Method, path),
		)
		http.Error(w, forbiddenMessage(resource, action), http.StatusForbidden)
		return
	}

	beforeParams := h.captureRackParamsIfNeeded(r, rackPath, rackConfig)

	status, err := h.forwardRequest(w, r, rackConfig, rackPath, authUser)
	if err != nil {
		h.handleForwardError(w, r, err, rackConfig.Name, start)
		return
	}

	if status == 0 {
		status = http.StatusOK
	}

	h.createAuditLogIfNeeded(r, authUser, path, status, start)
	h.logRequestToCloudWatch(r, authUser.Email, rackConfig.Name, status, start)
	h.handleSuccessAuditing(r, w, authUser, path, status, rackPath, envDiffs, beforeParams, rackConfig, start)

	r.Header.Del("X-Release-Created")
}

func (h *Handler) handleError(
	w http.ResponseWriter,
	r *http.Request,
	message string,
	status int,
	rack string,
	start time.Time,
) {
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

	emailForScope := userEmail
	if emailForScope == "anonymous" {
		emailForScope = ""
	}

	sentryutil.WithHTTPRequestScope(r, emailForScope, map[string]string{
		"component": "proxy",
		"rack":      h.rackName,
	}, func() {
		sentry.CaptureException(err)
	})
}

func (h *Handler) getRackConfig() (config.RackConfig, error) {
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			return config.RackConfig{}, errors.New("no rack configured")
		}
	}
	return rackConfig, nil
}

func (h *Handler) prepareProxyRequest(
	w http.ResponseWriter,
	r *http.Request,
	start time.Time,
) (config.RackConfig, *auth.User, bool, *deployApprovalTracker, []envutil.EnvDiff, error) {
	rackConfig, err := h.getRackConfig()
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, "default", start)
		return config.RackConfig{}, nil, false, nil, nil, err
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rackConfig.Name, start)
		return config.RackConfig{}, nil, false, nil, nil, errors.New("rack disabled")
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rackConfig.Name, start)
		return config.RackConfig{}, nil, false, nil, nil, errors.New("unauthorized")
	}

	path := r.URL.Path
	rackPath := rbac.NormalizeRackPath(path)

	if !h.isAllowedConvoxRoute(r, rackPath) {
		http.NotFound(w, r)
		return config.RackConfig{}, nil, false, nil, nil, errors.New("not found")
	}

	methodForRBAC := h.determineMethod(r)
	resource, action, ok := rbac.MatchRackRoute(methodForRBAC, rackPath)
	if !ok {
		h.auditLogger.LogRequest(
			r,
			authUser.Email,
			rackConfig.Name,
			"deny",
			http.StatusNotFound,
			time.Since(start),
			fmt.Errorf("unknown route: %s %s", methodForRBAC, path),
		)
		http.NotFound(w, r)
		return config.RackConfig{}, nil, false, nil, nil, errors.New("unknown route")
	}

	allowed, approvalTracker, err := h.checkPermissions(w, r, authUser, resource, action, rackConfig.Name, start)
	if err != nil {
		return config.RackConfig{}, nil, false, nil, nil, err
	}

	if approvalTracker != nil {
		ctx := context.WithValue(r.Context(), deployApprovalContextKey, approvalTracker)
		r = r.WithContext(ctx)
	}

	if !authUser.IsAPIToken {
		mfaErr := h.verifyMFAIfRequired(r, w, authUser, resource, action, &rackConfig, start)
		if mfaErr != nil {
			return config.RackConfig{}, nil, false, nil, nil, mfaErr
		}
	}

	envDiffs, err := h.prepareReleaseIfNeeded(
		r, w, allowed, rackPath, rackConfig, authUser.Email, resource, action, start,
	)
	if err != nil {
		return config.RackConfig{}, nil, false, nil, nil, err
	}

	if err := h.enforceDestructivePolicy(
		r, w, methodForRBAC, resource, action, authUser.Email, rackConfig.Name, start,
	); err != nil {
		return config.RackConfig{}, nil, false, nil, nil, err
	}

	if err := h.validateAuditRequirements(r, w, path, rackConfig.Name, start); err != nil {
		return config.RackConfig{}, nil, false, nil, nil, err
	}

	return rackConfig, authUser, allowed, approvalTracker, envDiffs, nil
}

func (h *Handler) handleForwardError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	rackName string,
	start time.Time,
) {
	if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
		logRackTLSMismatch("proxy_forward", fpErr)
		h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackName, start)
		return
	}
	h.handleError(w, r, err.Error(), http.StatusInternalServerError, rackName, start)
}

func (h *Handler) isAllowedConvoxRoute(r *http.Request, rackPath string) bool {
	methodForAllow := h.determineMethod(r)
	_, _, ok := rbac.MatchRackRoute(methodForAllow, rackPath)
	return ok
}

func (h *Handler) determineMethod(r *http.Request) string {
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		return "SOCKET"
	}
	return r.Method
}

func (h *Handler) checkPermissions(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	resource rbac.Resource,
	action rbac.Action,
	rackName string,
	start time.Time,
) (bool, *deployApprovalTracker, error) {
	var (
		allowed         bool
		approvalTracker *deployApprovalTracker
		err             error
	)

	if authUser.IsAPIToken {
		allowed, approvalTracker, err = h.evaluateAPITokenPermission(r, authUser, resource, action)
		if err != nil {
			return h.handlePermissionError(w, r, authUser, err, rackName, start)
		}
	} else {
		allowed, _ = h.checkUserPermissions(authUser, resource, action)
	}

	return allowed, approvalTracker, nil
}

func (h *Handler) handlePermissionError(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	err error,
	rackName string,
	start time.Time,
) (bool, *deployApprovalTracker, error) {
	if appErr, ok := err.(*deployApprovalError); ok {
		h.auditLogger.LogRequest(
			r,
			authUser.Email,
			rackName,
			"deny",
			appErr.status,
			time.Since(start),
			errors.New(appErr.message),
		)
		http.Error(w, appErr.message, appErr.status)
		return false, nil, err
	}
	h.handleError(
		w,
		r,
		"failed to validate deploy approval",
		http.StatusInternalServerError,
		rackName,
		start,
	)
	return false, nil, err
}

func (h *Handler) checkUserPermissions(
	authUser *auth.User,
	resource rbac.Resource,
	action rbac.Action,
) (bool, error) {
	if authUser != nil && authUser.DBUser != nil {
		return h.rbacManager.EnforceUser(authUser.DBUser, rbac.ScopeConvox, resource, action)
	}
	allowed, err := h.rbacManager.Enforce(authUser.Email, rbac.ScopeConvox, resource, action)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (h *Handler) prepareReleaseIfNeeded(
	r *http.Request,
	w http.ResponseWriter,
	allowed bool,
	rackPath string,
	rackConfig config.RackConfig,
	userEmail string,
	resource rbac.Resource,
	action rbac.Action,
	start time.Time,
) ([]envutil.EnvDiff, error) {
	if !allowed || r.Method != http.MethodPost || !strings.Contains(rackPath, "/releases") {
		return nil, nil
	}

	ok, diffs, err := h.prepareReleaseCreate(r, rackConfig, userEmail)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("env_fetch", fpErr)
			h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackConfig.Name, start)
			return nil, err
		}
		h.handleError(w, r, err.Error(), http.StatusBadRequest, rackConfig.Name, start)
		return nil, err
	}

	if !ok {
		http.Error(w, forbiddenMessage(resource, action), http.StatusForbidden)
		return nil, errors.New("release preparation denied")
	}

	return diffs, nil
}

func (h *Handler) enforceDestructivePolicy(
	r *http.Request,
	w http.ResponseWriter,
	method string,
	resource rbac.Resource,
	action rbac.Action,
	userEmail, rackName string,
	start time.Time,
) error {
	allowDestructive, err := h.settingsService.GetAllowDestructiveActions()
	if err != nil {
		log.Printf("Failed to get allow_destructive_actions setting: %v", err)
		allowDestructive = false
	}

	if !allowDestructive && isDestructive(method, resource, action) {
		h.auditLogger.LogRequest(
			r,
			userEmail,
			rackName,
			"deny",
			http.StatusForbidden,
			time.Since(start),
			fmt.Errorf("destructive actions are disabled by policy"),
		)
		http.Error(w, "Destructive rack actions are disabled by policy", http.StatusForbidden)
		return errors.New("destructive action blocked")
	}

	return nil
}

func (h *Handler) validateAuditRequirements(
	r *http.Request,
	w http.ResponseWriter,
	path, rackName string,
	start time.Time,
) error {
	if audit.HasAuditLogBeenCreated(r.Context()) {
		return nil
	}

	action, resource := h.auditLogger.ParseConvoxAction(path, r.Method, r.Header.Get("X-Audit-Resource"))
	if action == "unknown" || resource == "unknown" {
		return h.handleAuditValidationError(
			r, w, "cannot determine action/resource", action, resource, "", rackName, start,
		)
	}

	resourceType := h.auditLogger.InferResourceType(r.URL.Path, action)
	if resourceType == "unknown" {
		return h.handleAuditValidationError(
			r, w, "cannot determine resource type", action, resource, resourceType, rackName, start,
		)
	}

	return nil
}

func (h *Handler) handleAuditValidationError(
	r *http.Request,
	w http.ResponseWriter,
	message, action, resource, resourceType, rackName string,
	start time.Time,
) error {
	errorMsg := fmt.Sprintf("%s for %s %s", message, r.Method, r.URL.Path)
	logMsg := fmt.Sprintf(
		`{"level":"error","error":"audit_failure","message":"%s","method":"%s",`+
			`"path":"%s","action":"%s","resource":"%s"`,
		errorMsg, r.Method, r.URL.Path, action, resource,
	)
	if resourceType != "" {
		logMsg += fmt.Sprintf(`,"resource_type":"%s"`, resourceType)
	}
	logMsg += "}"
	log.Printf("%s", logMsg)

	h.handleError(w, r, errorMsg, http.StatusInternalServerError, rackName, start)
	return errors.New(errorMsg)
}

func (h *Handler) captureRackParamsIfNeeded(
	r *http.Request,
	rackPath string,
	rackConfig config.RackConfig,
) map[string]string {
	isRackParamsUpdate := (r.Method == http.MethodPut && rbac.KeyMatch3(rackPath, "/system"))
	if !isRackParamsUpdate {
		return nil
	}

	params, err := h.fetchSystemParams(r.Context(), rackConfig)
	if err != nil {
		return nil
	}
	return params
}

func (h *Handler) createAuditLogIfNeeded(
	r *http.Request,
	authUser *auth.User,
	path string,
	status int,
	start time.Time,
) {
	if audit.HasAuditLogBeenCreated(r.Context()) {
		return
	}

	action, resource := h.auditLogger.ParseConvoxAction(path, r.Method, r.Header.Get("X-Audit-Resource"))
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

func (h *Handler) logRequestToCloudWatch(
	r *http.Request,
	userEmail, rackName string,
	status int,
	start time.Time,
) {
	if !audit.RequestAlreadyLogged(r) {
		h.auditLogger.LogRequest(r, userEmail, rackName, "allow", status, time.Since(start), nil)
	}
}

func (h *Handler) handleSuccessAuditing(
	r *http.Request,
	_ http.ResponseWriter,
	authUser *auth.User,
	path string,
	status int,
	rackPath string,
	envDiffs []envutil.EnvDiff,
	beforeParams map[string]string,
	rackConfig config.RackConfig,
	start time.Time,
) {
	if status < 200 || status >= 300 {
		return
	}

	h.auditReleaseCreations(r, authUser, path, status, start)
	h.logEnvDiffs(r, authUser.Email, rackConfig.Name, envDiffs)
	h.auditRackParamsIfChanged(r, authUser.Email, rackPath, beforeParams, rackConfig)
}

func (h *Handler) auditReleaseCreations(
	r *http.Request,
	authUser *auth.User,
	path string,
	status int,
	start time.Time,
) {
	skipManualReleaseLog := r.Method == http.MethodPost && rbac.KeyMatch3(path, "/apps/{app}/releases")
	releaseIDs := r.Header.Values("X-Release-Created")

	for _, rel := range releaseIDs {
		rel = strings.TrimSpace(rel)
		if rel == "" || skipManualReleaseLog {
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
			Action:         audit.BuildAction(rbac.ResourceRelease.String(), rbac.ActionCreate.String()),
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

func (h *Handler) auditRackParamsIfChanged(
	r *http.Request,
	userEmail, rackPath string,
	beforeParams map[string]string,
	rackConfig config.RackConfig,
) {
	isRackParamsUpdate := (r.Method == http.MethodPut && rbac.KeyMatch3(rackPath, "/system"))
	if !isRackParamsUpdate || beforeParams == nil {
		return
	}

	afterParams, err := h.fetchSystemParams(r.Context(), rackConfig)
	if err != nil {
		return
	}

	changes := diffParams(beforeParams, afterParams)
	if len(changes) > 0 {
		h.notifyRackParamsChanged(r, userEmail, changes)
		h.auditRackParamsChanged(r, userEmail, changes)
	}
}
