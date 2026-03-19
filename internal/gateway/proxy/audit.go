package proxy

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/logutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

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
		errorMsg := fmt.Sprintf("cannot determine action/resource for %s %s",
			logutil.SanitizeForLog(r.Method), logutil.SanitizeForLog(r.URL.Path))
		log.Printf(
			`{"level":"error","error":"audit_failure","message":"%s","method":"%s","path":"%s",`+
				`"action":"%s","resource":"%s"}`,
			logutil.SanitizeForLog(errorMsg), logutil.SanitizeForLog(r.Method), logutil.SanitizeForLog(r.URL.Path),
			logutil.SanitizeForLog(action), logutil.SanitizeForLog(resource),
		)
		h.handleError(w, r, errorMsg, http.StatusInternalServerError, rackName, start)
		return errors.New(errorMsg)
	}

	resourceType := h.auditLogger.InferResourceType(r.URL.Path, action)
	if resourceType == "unknown" {
		errorMsg := fmt.Sprintf("cannot determine resource type for %s %s",
			logutil.SanitizeForLog(r.Method), logutil.SanitizeForLog(r.URL.Path))
		log.Printf(
			`{"level":"error","error":"audit_failure","message":"%s","method":"%s","path":"%s",`+
				`"action":"%s","resource":"%s","resource_type":"%s"}`,
			logutil.SanitizeForLog(errorMsg), logutil.SanitizeForLog(r.Method), logutil.SanitizeForLog(r.URL.Path),
			logutil.SanitizeForLog(action), logutil.SanitizeForLog(resource), logutil.SanitizeForLog(resourceType),
		)
		h.handleError(w, r, errorMsg, http.StatusInternalServerError, rackName, start)
		return errors.New(errorMsg)
	}

	return nil
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
