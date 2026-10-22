package handlers_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/handlers"
)

// createAuditEntries writes one audit entry per user agent. Each has its own action, so none are aggregated.
func createAuditEntries(t *testing.T, database *db.Database, userAgents ...string) {
	t.Helper()
	for i, userAgent := range userAgents {
		require.NoError(t, database.CreateAuditLog(&db.AuditLog{
			UserEmail:    "admin@example.com",
			UserName:     "Admin",
			ActionType:   "gateway",
			Action:       fmt.Sprintf("test.action_%d", i),
			ResourceType: "test",
			Resource:     fmt.Sprintf("resource-%d", i),
			IPAddress:    "10.0.0.1",
			UserAgent:    userAgent,
			Status:       "success",
			RBACDecision: "allow",
			HTTPStatus:   http.StatusOK,
		}))
	}
}

func exportAuditCSV(t *testing.T, handler *handlers.AdminHandler, query string) [][]string {
	t.Helper()
	c, w := newGinContext(http.MethodGet, "/api/v1/audit-logs/export?"+query, nil)
	attachAdminContext(c)
	handler.ExportAuditLogs(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	records, err := csv.NewReader(w.Body).ReadAll()
	require.NoError(t, err)
	return records
}

// An export holds every entry that matches, not one page of them.
func TestAuditExportHoldsEveryMatchingEntry(t *testing.T) {
	handler, database, _ := newAdminHandler(t)
	const entries = 250
	createAuditEntries(t, database, make([]string, entries)...)

	records := exportAuditCSV(t, handler, "range=all")
	require.Len(t, records, entries+1, "the header and every entry")
	require.Equal(t, "first_seen", records[0][0])
}

// Request text (a login's User-Agent needs no session) can't become a spreadsheet formula in an export.
func TestAuditExportCellsCannotRunAsFormulas(t *testing.T) {
	handler, database, _ := newAdminHandler(t)
	userAgents := []string{"=HYPERLINK(\"https://evil.example\")", "  +SUM(1,2)", "\t@cmd", "-2+3", "curl/8.0"}
	createAuditEntries(t, database, userAgents...)

	got := map[string]bool{}
	for _, record := range exportAuditCSV(t, handler, "range=all")[1:] {
		got[record[len(record)-1]] = true
	}
	for _, userAgent := range userAgents[:4] {
		require.True(t, got["'"+userAgent], "%q is exported as text: %v", userAgent, got)
	}
	require.True(t, got["curl/8.0"], "plain text is exported as it is")
}

// A page of the audit log holds at most 200 entries, and no page number overflows its offset.
func TestAuditListPageIsBounded(t *testing.T) {
	handler, _, _ := newAdminHandler(t)
	c, w := newGinContext(http.MethodGet, "/api/v1/audit-logs?range=all&limit=100000&page=99999999999", nil)
	attachAdminContext(c)
	handler.ListAuditLogs(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var response handlers.AuditLogsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, db.MaxAuditLogPageSize, response.Limit)
	require.Positive(t, response.Page)
}

func TestAuditSearchIsBounded(t *testing.T) {
	handler, _, _ := newAdminHandler(t)
	c, w := newGinContext(http.MethodGet, "/api/v1/audit-logs?search="+strings.Repeat("a", 201), nil)
	attachAdminContext(c)
	handler.ListAuditLogs(c)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "search is longer than 200 characters")
}
