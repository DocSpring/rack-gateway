package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
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
	secretNamesErr   error // CONVOX_SECRET_ENV_VARS can't be read: every env var counts as secret
	database         *db.Database
	settingsService  *settings.Service
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
		allowDestructive: false,
		emailer:          mailer,
		rackName:         rackName,
		rackAlias:        strings.TrimSpace(rackAlias),
		rackCertManager:  rackCertManager,
		mfaService:       mfaService,
		sessionManager:   sessionManager,
	}
	h.secretNamesErr = h.loadSecretNames()
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
		logutil.SanitizeForLog(scope),
		logutil.SanitizeForLog(err.Expected),
		logutil.SanitizeForLog(err.Actual),
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

// ProxyToRack handles proxying requests to the Convox rack with RBAC, MFA, and audit logging.
func (h *Handler) ProxyToRack(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	r, rackConfig, authUser, allowed, approvalTracker, envDiffs, err := h.prepareProxyRequest(w, r, start)
	if err != nil {
		return
	}

	// prepareProxyRequest made the request's path the rack path.
	path := r.URL.Path
	rackPath := path

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

	// Only an allowed request gets longer deadlines. Nothing before this point reads an upload's body.
	extendDeadlines(w, r, rackPath)

	if err := h.claimDeployApprovalStep(r, rackPath, approvalTracker); err != nil {
		_, _, _ = h.handlePermissionError(w, r, authUser, err, rackConfig.Name, start)
		return
	}

	beforeParams := h.captureRackParamsIfNeeded(r, rackPath, rackConfig)

	status, err := h.forwardRequest(w, r, rackConfig, rackPath, authUser)
	h.releaseDeployApprovalClaims(r, rackPath, status, err)
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

	// From here on the request's path is the rack path it's authorized and forwarded as.
	r, resource, action, err := h.resolveRackRoute(w, r, authUser.Email, rackConfig.Name, start)
	if err != nil {
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}
	path := r.URL.Path
	methodForRBAC := h.determineMethod(r)

	allowed, approvalTracker, err := h.checkPermissions(w, r, authUser, resource, action, rackConfig.Name, start)
	if err != nil {
		return r, config.RackConfig{}, nil, false, nil, nil, err
	}

	if approvalTracker != nil {
		ctx := context.WithValue(r.Context(), deployApprovalContextKey, approvalTracker)
		r = r.WithContext(ctx)
	}

	if !authUser.IsAPIToken {
		mfaErr := h.verifyMFAIfRequired(r, w, authUser, resource, action, &rackConfig, start)
		if mfaErr != nil {
			return r, config.RackConfig{}, nil, false, nil, nil, mfaErr
		}
	}

	envDiffs, err := h.prepareReleaseIfNeeded(
		r, w, allowed, path, rackConfig, authUser.Email, start,
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

// resolveRackRoute rewrites r to its canonical rack path and finds the RBAC resource and action for it. A path
// that isn't an allowed rack route is answered with 404, and an unknown route is also audited as a denial.
func (h *Handler) resolveRackRoute(
	w http.ResponseWriter,
	r *http.Request,
	userEmail, rackName string,
	start time.Time,
) (*http.Request, rbac.Resource, rbac.Action, error) {
	r, ok := canonicalRackRequest(r)
	if !ok || !h.isAllowedConvoxRoute(r, r.URL.Path) {
		http.NotFound(w, r)
		return r, 0, 0, errors.New("not found")
	}
	method := h.determineMethod(r)
	resource, action, ok := rbac.MatchRackRoute(method, r.URL.Path)
	if !ok {
		err := fmt.Errorf("unknown route: %s %s", method, r.URL.Path)
		h.auditLogger.LogRequest(r, userEmail, rackName, "deny", http.StatusNotFound, time.Since(start), err)
		http.NotFound(w, r)
		return r, 0, 0, errors.New("unknown route")
	}
	return r, resource, action, nil
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
	h.handleError(w, r, err.Error(), bodyErrorStatus(err, http.StatusInternalServerError), rackName, start)
}

func (h *Handler) isAllowedConvoxRoute(r *http.Request, rackPath string) bool {
	if !isSafeRackPath(rackPath) {
		return false
	}
	methodForAllow := h.determineMethod(r)
	_, _, ok := rbac.MatchRackRoute(methodForAllow, rackPath)
	return ok
}

func (_ *Handler) determineMethod(r *http.Request) string {
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
	if authUser == nil {
		return false, nil
	}
	return h.rbacManager.Authorize(authUser.Principal(), rbac.Convox(resource, action))
}

// callerCan reports whether the authenticated caller on r holds convox:<resource>:<action>.
func (h *Handler) callerCan(r *http.Request, resource rbac.Resource, action rbac.Action) bool {
	allowed, err := auth.Authorize(r.Context(), h.rbacManager, rbac.Convox(resource, action))
	if err != nil {
		gtwlog.Errorf("proxy: permission check failed for %s: %v", rbac.Convox(resource, action), err)
		return false
	}
	return allowed
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
	start time.Time,
) ([]envutil.EnvDiff, error) {
	if !allowed || r.Method != http.MethodPost || !strings.Contains(rackPath, "/releases") {
		return nil, nil
	}

	diffs, err := h.prepareReleaseCreate(r, rackConfig, userEmail)
	if err == nil {
		return diffs, nil
	}
	if writeEnvChangeRefusal(w, err) {
		return nil, err
	}
	if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
		logRackTLSMismatch("env_fetch", fpErr)
		h.handleError(w, r, "rack certificate verification failed", http.StatusBadGateway, rackConfig.Name, start)
		return nil, err
	}
	h.handleError(w, r, err.Error(), bodyErrorStatus(err, http.StatusBadRequest), rackConfig.Name, start)
	return nil, err
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
