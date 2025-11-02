package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func (h *AdminHandler) auditFiltersFromRequest(c *gin.Context) (db.AuditLogFilters, int, int, error) {
	page, limit := parsePaginationParams(c)
	userFilter, missingUserForID, err := h.resolveUserFilter(c)
	if err != nil {
		return db.AuditLogFilters{}, 0, 0, err
	}

	statusFilter := c.Query("status")
	actionTypeFilter := c.Query("action_type")
	resourceTypeFilter := c.Query("resource_type")
	searchFilter := c.Query("search")
	rangeFilter := strings.TrimSpace(c.DefaultQuery("range", "24h"))
	startParam := c.Query("start")
	endParam := c.Query("end")

	since, until, hasStart, hasEnd, err := parseTimeRange(startParam, endParam)
	if err != nil {
		return db.AuditLogFilters{}, 0, 0, err
	}

	since, hasStart, rangeFilter = applyRangeDefault(since, hasStart, rangeFilter)

	filters := buildAuditLogFilters(
		userFilter, statusFilter, actionTypeFilter, resourceTypeFilter,
		searchFilter, rangeFilter, page, limit,
		since, until, hasStart, hasEnd, missingUserForID, c,
	)

	return filters, page, limit, nil
}

func parsePaginationParams(c *gin.Context) (int, int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	return page, limit
}

func (h *AdminHandler) resolveUserFilter(c *gin.Context) (string, bool, error) {
	userFilter := c.Query("user")
	if userFilter != "" {
		return userFilter, false, nil
	}

	userIDParam := c.Query("user_id")
	if userIDParam == "" {
		return "", false, nil
	}

	userID, convErr := strconv.ParseInt(userIDParam, 10, 64)
	if convErr != nil {
		return "", false, nil
	}

	user, lookupErr := h.database.GetUserByID(userID)
	if lookupErr != nil {
		return "", false, lookupErr
	}

	if user != nil {
		return user.Email, false, nil
	}

	return "", true, nil
}

func parseTimeRange(startParam, endParam string) (time.Time, time.Time, bool, bool, error) {
	var since, until time.Time
	var hasStart, hasEnd bool
	var startError, endError error

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
		return time.Time{}, time.Time{}, false, false, errInvalidStartTime
	}
	if endError != nil {
		return time.Time{}, time.Time{}, false, false, errInvalidEndTime
	}
	if hasStart && hasEnd && until.Before(since) {
		return time.Time{}, time.Time{}, false, false, errInvalidTimeRange
	}

	return since, until, hasStart, hasEnd, nil
}

func applyRangeDefault(since time.Time, hasStart bool, rangeFilter string) (time.Time, bool, string) {
	if hasStart {
		if rangeFilter == "" {
			rangeFilter = "custom"
		}
		return since, hasStart, rangeFilter
	}

	now := time.Now()
	switch rangeFilter {
	case "15m":
		return now.Add(-15 * time.Minute), true, rangeFilter
	case "1h":
		return now.Add(-1 * time.Hour), true, rangeFilter
	case "24h":
		return now.Add(-24 * time.Hour), true, rangeFilter
	case "7d":
		return now.Add(-7 * 24 * time.Hour), true, rangeFilter
	case "30d":
		return now.Add(-30 * 24 * time.Hour), true, rangeFilter
	case "all", "custom":
		return since, false, rangeFilter
	default:
		return now.Add(-24 * time.Hour), true, rangeFilter
	}
}

func buildAuditLogFilters(
	userFilter, statusFilter, actionTypeFilter, resourceTypeFilter,
	searchFilter, rangeFilter string, page, limit int,
	since, until time.Time, hasStart, hasEnd, missingUserForID bool, c *gin.Context,
) db.AuditLogFilters {
	resolvedUserEmail := userFilter
	if missingUserForID {
		resolvedUserEmail = fmt.Sprintf("__missing_user_%s__", strings.TrimSpace(c.Query("user_id")))
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	filters := db.AuditLogFilters{
		UserEmail:    resolvedUserEmail,
		Status:       statusFilter,
		ActionType:   actionTypeFilter,
		ResourceType: resourceTypeFilter,
		Search:       searchFilter,
		Range:        rangeFilter,
		Limit:        limit,
		Offset:       offset,
	}

	if hasStart {
		filters.Since = since
	}
	if hasEnd {
		filters.Until = until
	}

	return filters
}
