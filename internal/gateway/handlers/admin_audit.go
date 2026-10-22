package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// AdminHandler is defined in admin.go
// AuditLogsResponse is defined in dto.go
// cloneDetails and parseAuditTime are defined in admin_helpers.go

var (
	errInvalidStartTime   = errors.New("invalid start time")
	errInvalidEndTime     = errors.New("invalid end time")
	errInvalidTimeRange   = errors.New("end time must be after start time")
	errAuditSearchTooLong = fmt.Errorf("search is longer than %d characters", maxAuditSearchLength)
)

// auditFilterError is the status and message for filters auditFiltersFromRequest refused.
func auditFilterError(err error) (int, string) {
	refused := []error{errInvalidStartTime, errInvalidEndTime, errInvalidTimeRange, errAuditSearchTooLong}
	for _, invalid := range refused {
		if errors.Is(err, invalid) {
			return http.StatusBadRequest, invalid.Error()
		}
	}
	return http.StatusInternalServerError, "failed to fetch audit logs"
}

// ListAuditLogs godoc
// @Summary List audit logs
// @Description Returns paginated audit logs filtered by optional query parameters.
// @Tags Audit
// @Produce json
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param page query int false "Page number"
// @Param limit query int false "Page size (at most 200)"
// @Param start query string false "ISO8601 start time"
// @Param end query string false "ISO8601 end time"
// @Param range query string false "Relative range (e.g. 24h, 7d, custom)"
// @Param user query string false "Filter by user email"
// @Param user_id query string false "Filter by user ID"
// @Success 200 {object} AuditLogsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /audit-logs [get]
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	h.respondWithAuditLogs(c, "")
}

// respondWithAuditLogs responds with the filtered, paginated audit log. A non-empty userEmail restricts the
// results to that user's activity, overriding any user filter in the query string.
func (h *AdminHandler) respondWithAuditLogs(c *gin.Context, userEmail string) {
	start := time.Now()
	filters, page, limit, err := h.auditFiltersFromRequest(c)
	if err != nil {
		status, message := auditFilterError(err)
		h.respondAuditError(c, status, "audit.list", "", message, start, nil)
		return
	}
	if userEmail != "" {
		filters.UserEmail = userEmail
	}

	logs, total, err := h.database.GetAuditLogsAggregated(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.list", "", err.Error(), start, nil)
		return
	}

	eventTotal := 0
	for _, log := range logs {
		eventTotal += log.EventCount
	}

	payload := AuditLogsResponse{
		Logs:  logs,
		Total: total,
		Page:  page,
		Limit: limit,
	}

	baseDetails := map[string]interface{}{
		"total":       total,
		"event_total": eventTotal,
		"page":        page,
		"limit":       limit,
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, "audit.list", "", start, buildAuditDetails(filters, baseDetails))
}

func (h *AdminHandler) auditAdminAction(
	c *gin.Context,
	action, resource, status string,
	httpStatus int,
	details map[string]interface{},
	start time.Time,
) {
	if h == nil || h.database == nil || action == "audit.list" {
		return
	}

	email, name := extractAuditUserInfo(c)
	detailsJSON := marshalAuditDetails(details)
	actionType, resourceType := auditEntryTypes(c, action)

	entry := &db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     actionType,
		Action:         action,
		Resource:       strings.TrimSpace(resource),
		ResourceType:   resourceType,
		Details:        detailsJSON,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Status:         status,
		HTTPStatus:     httpStatus,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		RBACDecision:   mapStatusToRBAC(status),
	}

	_ = h.auditLogger.LogRequestEntry(c.Request.Context(), entry)
}

func extractAuditUserInfo(c *gin.Context) (string, string) {
	email := strings.TrimSpace(c.GetString("user_email"))
	name := strings.TrimSpace(c.GetString("user_name"))

	if au, ok := auth.GetAuthUser(c.Request.Context()); ok && au != nil {
		if e := strings.TrimSpace(au.Email); e != "" {
			email = e
		}
		if n := strings.TrimSpace(au.Name); n != "" {
			name = n
		}
	}

	return email, name
}

func marshalAuditDetails(details map[string]interface{}) string {
	detailsCopy := cloneDetails(details)
	if len(detailsCopy) == 0 {
		return ""
	}

	if payload, err := json.Marshal(detailsCopy); err == nil {
		return string(payload)
	}

	return ""
}

// auditEntryTypes returns the action type and resource type of an admin handler's audit entry, as its request's
// attempt entry has them: the scope of the route's permission, and the action's resource.
func auditEntryTypes(c *gin.Context, action string) (actionType, resourceType string) {
	actionType = audit.ActionTypeGateway
	if spec, ok := rbac.LookupHTTPRoute(c.Request.Method, c.FullPath()); ok {
		actionType = spec.AuditName(c.Request.URL.Query()).ActionType
	}
	resourceType, _, _ = strings.Cut(action, ".")
	return actionType, resourceType
}

func mapStatusToRBAC(status string) string {
	switch status {
	case "denied":
		return "deny"
	case "success":
		return "allow"
	default:
		return ""
	}
}

func buildAuditDetails(filters db.AuditLogFilters, base map[string]interface{}) map[string]interface{} {
	details := make(map[string]interface{}, len(base)+6)
	for key, value := range base {
		details[key] = value
	}

	if value := strings.TrimSpace(filters.ActionType); value != "" {
		details["action_type"] = value
	}
	if value := strings.TrimSpace(filters.Status); value != "" {
		details["status_filter"] = value
	}
	if value := strings.TrimSpace(filters.ResourceType); value != "" {
		details["resource_type"] = value
	}
	if value := strings.TrimSpace(filters.Search); value != "" {
		details["search"] = value
	}

	if !filters.Since.IsZero() {
		details["since"] = filters.Since.UTC().Format(time.RFC3339)
	}
	if !filters.Until.IsZero() {
		details["until"] = filters.Until.UTC().Format(time.RFC3339)
	}

	return details
}

func (h *AdminHandler) respondAudit(
	c *gin.Context,
	statusCode int,
	payload interface{},
	action, resource, auditStatus string,
	start time.Time,
	details map[string]interface{},
) {
	if payload == nil {
		c.Status(statusCode)
	} else {
		c.JSON(statusCode, payload)
	}
	h.auditAdminAction(c, action, resource, auditStatus, statusCode, details, start)
}

func (h *AdminHandler) respondAuditSuccess(
	c *gin.Context,
	statusCode int,
	payload interface{},
	action, resource string,
	start time.Time,
	details map[string]interface{},
) {
	h.respondAudit(c, statusCode, payload, action, resource, "success", start, details)
}

func (h *AdminHandler) respondAuditError(
	c *gin.Context,
	statusCode int,
	action, resource, message string,
	start time.Time,
	details map[string]interface{},
) {
	det := cloneDetails(details)
	if det == nil {
		det = make(map[string]interface{})
	}
	if message != "" {
		det["error"] = message
	}
	auditStatus := "error"
	if statusCode == http.StatusForbidden || statusCode == http.StatusUnauthorized {
		auditStatus = "denied"
	}
	h.respondAudit(c, statusCode, gin.H{"error": message}, action, resource, auditStatus, start, det)
}
