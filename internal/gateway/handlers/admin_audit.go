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

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/gin-gonic/gin"
)

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
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "end time must be after start time", start, nil)
		default:
			h.respondAuditError(c, http.StatusInternalServerError, "audit.list", "", "failed to fetch audit logs", start, nil)
		}
		return
	}

	logs, total, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.list", "", err.Error(), start, nil)
		return
	}

	eventTotal := 0
	for _, log := range logs {
		if log.EventCount > 0 {
			eventTotal += log.EventCount
		} else {
			eventTotal++
		}
	}

	payload := AuditLogsResponse{
		Logs:  logs,
		Total: total,
		Page:  page,
		Limit: limit,
	}

	details := map[string]interface{}{
		"total":         total,
		"event_total":   eventTotal,
		"page":          page,
		"limit":         limit,
		"action_type":   filters.ActionType,
		"status_filter": filters.Status,
		"resource_type": filters.ResourceType,
		"search":        filters.Search,
	}
	if !filters.Since.IsZero() {
		details["since"] = filters.Since.UTC().Format(time.RFC3339)
	}
	if !filters.Until.IsZero() {
		details["until"] = filters.Until.UTC().Format(time.RFC3339)
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, "audit.list", "", start, details)
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
		switch {
		case errors.Is(err, errInvalidStartTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "invalid start time", start, nil)
		case errors.Is(err, errInvalidEndTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "invalid end time", start, nil)
		case errors.Is(err, errInvalidTimeRange):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "end time must be after start time", start, nil)
		default:
			h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to fetch logs", start, nil)
		}
		return
	}

	if filters.Limit <= 0 || filters.Limit > 10000 {
		filters.Limit = 10000
	}
	filters.Offset = 0

	logs, _, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", err.Error(), start, nil)
		return
	}

	// Set CSV headers
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-logs-%s.csv\"", time.Now().Format("2006-01-02")))

	// Write CSV
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Header row
	header := []string{
		"timestamp", "user_email", "user_name", "action_type", "action",
		"command", "resource", "status", "event_count", "response_time_ms", "ip_address", "user_agent",
	}
	if err := writer.Write(header); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to write CSV header", start, nil)
		return
	}

	// Data rows
	totalEvents := 0
	for _, log := range logs {
		count := log.EventCount
		if count <= 0 {
			count = 1
		}
		totalEvents += count
		row := []string{
			log.Timestamp.Format(time.RFC3339),
			log.UserEmail,
			log.UserName,
			log.ActionType,
			log.Action,
			log.Command,
			log.Resource,
			log.Status,
			strconv.Itoa(count),
			strconv.Itoa(log.ResponseTimeMs),
			log.IPAddress,
			log.UserAgent,
		}
		if err := writer.Write(row); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to write CSV row", start, nil)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to flush CSV", start, nil)
		return
	}

	details := map[string]interface{}{
		"count":         len(logs),
		"events":        totalEvents,
		"action_type":   filters.ActionType,
		"status_filter": filters.Status,
		"resource_type": filters.ResourceType,
		"search":        filters.Search,
	}
	if !filters.Since.IsZero() {
		details["since"] = filters.Since.UTC().Format(time.RFC3339)
	}
	if !filters.Until.IsZero() {
		details["until"] = filters.Until.UTC().Format(time.RFC3339)
	}

	h.auditAdminAction(c, "audit.export", "", "success", http.StatusOK, details, start)
}

func (h *AdminHandler) auditAdminAction(c *gin.Context, action, resource, status string, httpStatus int, details map[string]interface{}, start time.Time) {
	if h == nil || h.database == nil {
		return
	}

	if action == "audit.list" {
		return
	}

	trimmedResource := strings.TrimSpace(resource)
	detailsCopy := cloneDetails(details)
	var detailsJSON string
	if len(detailsCopy) > 0 {
		if payload, err := json.Marshal(detailsCopy); err == nil {
			detailsJSON = string(payload)
		}
	}

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

	actionType := "admin"
	switch {
	case strings.HasPrefix(action, "api_token."):
		actionType = "tokens"
	case strings.HasPrefix(action, "user."):
		actionType = "users"
	case strings.HasPrefix(action, "audit."):
		actionType = "admin"
	}

	resourceType := "admin"
	switch {
	case strings.HasPrefix(action, "api_token."):
		resourceType = "api_token"
	case strings.HasPrefix(action, "user."):
		resourceType = "user"
	case strings.HasPrefix(action, "audit."):
		resourceType = "admin"
	}

	entry := &db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     actionType,
		Action:         action,
		Resource:       trimmedResource,
		ResourceType:   resourceType,
		Details:        detailsJSON,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Status:         status,
		HTTPStatus:     httpStatus,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
	}

	switch status {
	case "denied":
		entry.RBACDecision = "deny"
	case "success":
		entry.RBACDecision = "allow"
	}

	_ = h.auditLogger.LogDBEntry(entry)
}

func (h *AdminHandler) respondAudit(c *gin.Context, statusCode int, payload interface{}, action, resource, auditStatus string, start time.Time, details map[string]interface{}) {
	if payload == nil {
		c.Status(statusCode)
	} else {
		c.JSON(statusCode, payload)
	}
	h.auditAdminAction(c, action, resource, auditStatus, statusCode, details, start)
}

func (h *AdminHandler) respondAuditSuccess(c *gin.Context, statusCode int, payload interface{}, action, resource string, start time.Time, details map[string]interface{}) {
	h.respondAudit(c, statusCode, payload, action, resource, "success", start, details)
}

func (h *AdminHandler) respondAuditError(c *gin.Context, statusCode int, action, resource, message string, start time.Time, details map[string]interface{}) {
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

func (h *AdminHandler) auditFiltersFromRequest(c *gin.Context) (db.AuditLogFilters, int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		limit = 100
	}

	userFilter := c.Query("user")
	if userFilter == "" {
		if userIDParam := c.Query("user_id"); userIDParam != "" {
			if userID, convErr := strconv.ParseInt(userIDParam, 10, 64); convErr == nil {
				user, lookupErr := h.database.GetUserByID(userID)
				if lookupErr != nil {
					return db.AuditLogFilters{}, 0, 0, lookupErr
				}
				if user != nil {
					userFilter = user.Email
				}
			}
		}
	}

	statusFilter := c.Query("status")
	actionTypeFilter := c.Query("action_type")
	resourceTypeFilter := c.Query("resource_type")
	searchFilter := c.Query("search")
	rangeFilter := strings.TrimSpace(c.DefaultQuery("range", "24h"))
	startParam := c.Query("start")
	endParam := c.Query("end")
	missingUserForID := false

	var (
		since      time.Time
		until      time.Time
		hasStart   bool
		hasEnd     bool
		startError error
		endError   error
	)

	if strings.TrimSpace(startParam) != "" {
		parsed, parseErr := parseAuditTime(startParam)
		if parseErr != nil {
			startError = parseErr
		} else {
			since = parsed
			hasStart = true
		}
	}
	if strings.TrimSpace(endParam) != "" {
		parsed, parseErr := parseAuditTime(endParam)
		if parseErr != nil {
			endError = parseErr
		} else {
			until = parsed
			hasEnd = true
		}
	}

	if startError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidStartTime
	}
	if endError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidEndTime
	}
	if hasStart && hasEnd && until.Before(since) {
		return db.AuditLogFilters{}, 0, 0, errInvalidTimeRange
	}

	if !hasStart {
		now := time.Now()
		switch rangeFilter {
		case "15m":
			since = now.Add(-15 * time.Minute)
			hasStart = true
		case "1h":
			since = now.Add(-1 * time.Hour)
			hasStart = true
		case "24h":
			since = now.Add(-24 * time.Hour)
			hasStart = true
		case "7d":
			since = now.Add(-7 * 24 * time.Hour)
			hasStart = true
		case "30d":
			since = now.Add(-30 * 24 * time.Hour)
			hasStart = true
		case "all":
			// no lower bound
		case "custom":
			// rely on explicit start/end parameters
		default:
			// fallback to 24h
			since = now.Add(-24 * time.Hour)
			hasStart = true
		}
	} else {
		// Ensure "custom" is reflected for URL sync if explicit start is provided without range
		if rangeFilter == "" {
			rangeFilter = "custom"
		}
	}

	resolvedUserEmail := userFilter
	if userFilter == "" && c.Query("user_id") != "" {
		missingUserForID = true
	}

	filters := db.AuditLogFilters{
		UserEmail:    resolvedUserEmail,
		Status:       statusFilter,
		ActionType:   actionTypeFilter,
		ResourceType: resourceTypeFilter,
		Search:       searchFilter,
		Range:        rangeFilter,
		Limit:        limit,
		Offset:       (page - 1) * limit,
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}
	if hasStart {
		filters.Since = since
	}
	if hasEnd {
		filters.Until = until
	}
	if missingUserForID {
		filters.UserEmail = fmt.Sprintf("__missing_user_%s__", strings.TrimSpace(c.Query("user_id")))
	}

	return filters, page, limit, nil
}
