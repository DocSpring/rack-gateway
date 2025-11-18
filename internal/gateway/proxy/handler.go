package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
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

	r, rackConfig, authUser, allowed, _, envDiffs, err := h.prepareProxyRequest(w, r, start)
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
) (*http.Request, config.RackConfig, *auth.User, bool, *deployApprovalTracker, []envutil.EnvDiff, error) {
	rackConfig, err := h.getRackConfig()
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, "default", start)
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rackConfig.Name, start)
		return r, config.RackConfig{}, nil, false, nil, nil, errors.New("rack disabled")
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rackConfig.Name, start)
		return r, config.RackConfig{}, nil, false, nil, nil, errors.New("unauthorized")
	}

	path := r.URL.Path
	rackPath := rbac.NormalizeRackPath(path)

	if !h.isAllowedConvoxRoute(r, rackPath) {
		http.NotFound(w, r)
		return r, config.RackConfig{}, nil, false, nil, nil, errors.New("not found")
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
		return r, config.RackConfig{}, nil, false, nil, nil, errors.New("unknown route")
	}

	allowed, approvalTracker, err := h.checkPermissions(w, r, authUser, resource, action, rackConfig.Name, start)
	if err != nil {
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}

	if approvalTracker != nil {
		log.Printf(
			"[CONTEXT] Setting approvalTracker in context: approval_id=%d tokenID=%d app=%s resource=%s action=%s path=%s",
			approvalTracker.request.ID,
			approvalTracker.tokenID,
			approvalTracker.app,
			resource,
			action,
			r.URL.Path,
		)
		ctx := context.WithValue(r.Context(), deployApprovalContextKey, approvalTracker)
		r = r.WithContext(ctx)
	} else {
		log.Printf(
			"[CONTEXT] NO approvalTracker to set in context: resource=%s action=%s path=%s",
			resource,
			action,
			r.URL.Path,
		)
	}

	if !authUser.IsAPIToken {
		mfaErr := h.verifyMFAIfRequired(r, w, authUser, resource, action, &rackConfig, start)
		if mfaErr != nil {
			return r, config.RackConfig{}, nil, false, nil, nil, mfaErr
		}
	}

	envDiffs, err := h.prepareReleaseIfNeeded(
		r, w, allowed, rackPath, rackConfig, authUser.Email, resource, action, start,
	)
	if err != nil {
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}

	// Only enforce destructive policy if permissions were granted
	// This ensures permission-denied errors take precedence over policy errors
	if allowed {
		if err := h.enforceDestructivePolicy(
			r, w, methodForRBAC, resource, action, authUser.Email, rackConfig.Name, start,
		); err != nil {
			return r, config.RackConfig{}, nil, false, nil, nil, err
		}
	}

	if err := h.validateAuditRequirements(r, w, path, rackConfig.Name, start); err != nil {
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}

	return r, rackConfig, authUser, allowed, approvalTracker, envDiffs, nil
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
