package handlers

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// AdminHandler is defined in admin.go
// AuditLogsResponse is defined in dto.go
// cloneDetails and parseAuditTime are defined in admin_helpers.go

var (
	errInvalidStartTime = errors.New("invalid start time")
	errInvalidEndTime   = errors.New("invalid end time")
	errInvalidTimeRange = errors.New("end time must be after start time")
)

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
// @Param limit query int false "Page size"
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
	start := time.Now()
	filters, page, limit, err := h.auditFiltersFromRequest(c)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidStartTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "invalid start time", start, nil)
		case errors.Is(err, errInvalidEndTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "invalid end time", start, nil)
		case errors.Is(err, errInvalidTimeRange):
			h.respondAuditError(
				c,
				http.StatusBadRequest,
				"audit.list",
				"",
				"end time must be after start time",
				start,
				nil,
			)
		default:
			h.respondAuditError(
				c,
				http.StatusInternalServerError,
				"audit.list",
				"",
				"failed to fetch audit logs",
				start,
				nil,
			)
		}
		return
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

// ExportAuditLogs godoc
// @Summary Export audit logs as CSV
// @Description Streams the filtered audit log dataset as CSV.
// @Tags Audit
// @Produce text/csv
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param since query string false "ISO8601 start time"
// @Param until query string false "ISO8601 end time"
// @Success 200 {file} binary
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /audit-logs/export [get]
func (h *AdminHandler) ExportAuditLogs(c *gin.Context) {
	start := time.Now()
	filters, _, _, err := h.auditFiltersFromRequest(c)
	if err != nil {
		h.handleExportFilterError(c, err, start)
		return
	}

	if filters.Limit <= 0 || filters.Limit > 10000 {
		filters.Limit = 10000
	}
	filters.Offset = 0

	logs, _, err := h.database.GetAuditLogsAggregated(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", err.Error(), start, nil)
		return
	}

	h.setCSVResponseHeaders(c)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	if err := h.writeAuditCSVHeader(c, writer, start); err != nil {
		return
	}

	totalEvents, err := h.writeAuditCSVRows(c, writer, logs, start)
	if err != nil {
		return
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to flush CSV", start, nil)
		return
	}

	baseDetails := map[string]interface{}{
		"count":  len(logs),
		"events": totalEvents,
	}

	h.auditAdminAction(c, "audit.export", "", "success", http.StatusOK, buildAuditDetails(filters, baseDetails), start)
}

func (h *AdminHandler) handleExportFilterError(c *gin.Context, err error, start time.Time) {
	switch {
	case errors.Is(err, errInvalidStartTime):
		h.respondAuditError(
			c, http.StatusBadRequest, "audit.export", "",
			"invalid start time", start, nil,
		)
	case errors.Is(err, errInvalidEndTime):
		h.respondAuditError(
			c, http.StatusBadRequest, "audit.export", "",
			"invalid end time", start, nil,
		)
	case errors.Is(err, errInvalidTimeRange):
		h.respondAuditError(
			c, http.StatusBadRequest, "audit.export", "",
			"end time must be after start time", start, nil,
		)
	default:
		h.respondAuditError(
			c, http.StatusInternalServerError, "audit.export", "",
			"failed to fetch logs", start, nil,
		)
	}
}

func (h *AdminHandler) setCSVResponseHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/csv")
	filename := fmt.Sprintf(
		"attachment; filename=\"audit-logs-%s.csv\"",
		time.Now().Format("2006-01-02"),
	)
	c.Header("Content-Disposition", filename)
}

func (h *AdminHandler) writeAuditCSVHeader(
	c *gin.Context, writer *csv.Writer, start time.Time,
) error {
	header := []string{
		"first_seen", "last_seen", "user_email", "user_name", "action_type", "action",
		"command", "resource", "status", "event_count",
		"min_response_ms", "max_response_ms", "avg_response_ms", "ip_address", "user_agent",
	}
	if err := writer.Write(header); err != nil {
		h.respondAuditError(
			c, http.StatusInternalServerError, "audit.export", "",
			"failed to write CSV header", start, nil,
		)
		return err
	}
	return nil
}

func (h *AdminHandler) writeAuditCSVRows(
	c *gin.Context, writer *csv.Writer,
	logs []*db.AuditLogAggregated, start time.Time,
) (int, error) {
	totalEvents := 0
	for _, log := range logs {
		totalEvents += log.EventCount
		row := []string{
			log.FirstSeen.Format(time.RFC3339),
			log.LastSeen.Format(time.RFC3339),
			log.UserEmail,
			log.UserName,
			log.ActionType,
			log.Action,
			log.Command,
			log.Resource,
			log.Status,
			strconv.Itoa(log.EventCount),
			strconv.Itoa(log.MinResponseTimeMs),
			strconv.Itoa(log.MaxResponseTimeMs),
			strconv.Itoa(log.AvgResponseTimeMs),
			log.IPAddress,
			log.UserAgent,
		}
		if err := writer.Write(row); err != nil {
			h.respondAuditError(
				c, http.StatusInternalServerError, "audit.export", "",
				"failed to write CSV row", start, nil,
			)
			return 0, err
		}
	}
	return totalEvents, nil
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
	actionType, resourceType := classifyAuditAction(action)

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

	_ = h.auditLogger.LogDBEntry(entry)
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

func classifyAuditAction(action string) (actionType, resourceType string) {
	switch {
	case strings.HasPrefix(action, "api_token."):
		return "tokens", "api_token"
	case strings.HasPrefix(action, "user."):
		return "users", "user"
	default:
		return "admin", "admin"
	}
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
