package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

const (
	// auditExportTimeout bounds reading an export's entries from the database.
	auditExportTimeout = 5 * time.Minute
	// auditExportSendTimeout is how long the client has to download a finished export.
	auditExportSendTimeout = 5 * time.Minute
)

// auditCSVHeader names the columns of an audit log export.
var auditCSVHeader = []string{
	"first_seen", "last_seen", "user_email", "user_name", "action_type", "action",
	"command", "resource", "status", "event_count",
	"min_response_ms", "max_response_ms", "avg_response_ms", "ip_address", "user_agent",
}

// ExportAuditLogs godoc
// @Summary Export audit logs as CSV
// @Description Exports every audit log entry that matches the filters as CSV. The export is written in full before
// @Description any of it is sent, so a failed export answers with an error, never with a partial file.
// @Tags Audit
// @Produce text/csv
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param start query string false "ISO8601 start time"
// @Param end query string false "ISO8601 end time"
// @Param range query string false "Relative range (e.g. 24h, 7d, all, custom)"
// @Success 200 {file} binary
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /audit-logs/export [get]
func (h *AdminHandler) ExportAuditLogs(c *gin.Context) {
	start := time.Now()
	filters, _, _, err := h.auditFiltersFromRequest(c)
	if err != nil {
		status, message := auditFilterError(err)
		h.respondAuditError(c, status, "audit.export", "", message, start, nil)
		return
	}
	// An export can take longer than the server-wide write timeout.
	deadline := time.Now().Add(auditExportTimeout + auditExportSendTimeout)
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(deadline); err != nil {
		log.Printf("audit export: can't extend the write deadline: %v", err)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), auditExportTimeout)
	defer cancel()
	export, err := spoolAuditCSV(ctx, h.database, filters)
	if err != nil {
		log.Printf("audit export failed: %v", err)
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to export audit logs",
			start, nil)
		return
	}
	defer export.remove()

	disposition := fmt.Sprintf("attachment; filename=\"audit-logs-%s.csv\"", time.Now().Format("2006-01-02"))
	c.DataFromReader(http.StatusOK, export.size, "text/csv", export.file,
		map[string]string{"Content-Disposition": disposition})
	details := buildAuditDetails(filters, map[string]interface{}{"count": export.entries, "events": export.events})
	h.auditAdminAction(c, "audit.export", "", "success", http.StatusOK, details, start)
}

// auditCSVExport is an audit log export written to a temporary file.
type auditCSVExport struct {
	file    *os.File
	size    int64
	entries int
	events  int
}

// spoolAuditCSV writes every audit log entry that matches filters to a temporary CSV file, ready to be read from
// the start.
func spoolAuditCSV(ctx context.Context, database *db.Database, filters db.AuditLogFilters) (*auditCSVExport, error) {
	file, err := os.CreateTemp("", "audit-export-*.csv")
	if err != nil {
		return nil, fmt.Errorf("failed to create the export file: %w", err)
	}
	export := &auditCSVExport{file: file}
	if err := export.write(ctx, database, filters); err != nil {
		export.remove()
		return nil, err
	}
	return export, nil
}

func (e *auditCSVExport) write(ctx context.Context, database *db.Database, filters db.AuditLogFilters) error {
	writer := csv.NewWriter(e.file)
	if err := writer.Write(auditCSVHeader); err != nil {
		return err
	}
	err := database.EachAuditLogAggregated(ctx, filters, func(entry *db.AuditLogAggregated) error {
		e.entries++
		e.events += entry.EventCount
		return writer.Write(auditCSVRow(entry))
	})
	if err != nil {
		return err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if e.size, err = e.file.Seek(0, io.SeekCurrent); err != nil {
		return err
	}
	_, err = e.file.Seek(0, io.SeekStart)
	return err
}

func (e *auditCSVExport) remove() {
	_ = e.file.Close()
	_ = os.Remove(e.file.Name())
}

// auditCSVRow is an entry's row in an export. Its text comes from requests (a login's User-Agent needs no
// session), so every text cell is made spreadsheet-safe.
func auditCSVRow(entry *db.AuditLogAggregated) []string {
	return []string{
		entry.FirstSeen.Format(time.RFC3339),
		entry.LastSeen.Format(time.RFC3339),
		spreadsheetSafe(entry.UserEmail),
		spreadsheetSafe(entry.UserName),
		spreadsheetSafe(entry.ActionType),
		spreadsheetSafe(entry.Action),
		spreadsheetSafe(entry.Command),
		spreadsheetSafe(entry.Resource),
		spreadsheetSafe(entry.Status),
		strconv.Itoa(entry.EventCount),
		strconv.Itoa(entry.MinResponseTimeMs),
		strconv.Itoa(entry.MaxResponseTimeMs),
		strconv.Itoa(entry.AvgResponseTimeMs),
		spreadsheetSafe(entry.IPAddress),
		spreadsheetSafe(entry.UserAgent),
	}
}

// spreadsheetSafe keeps a CSV cell from being run as a formula. Spreadsheets evaluate a cell that starts with =, +,
// - or @, even after leading whitespace or control characters, and CSV quoting doesn't stop them. Such a cell gets
// a leading apostrophe, which makes spreadsheets show it as text.
func spreadsheetSafe(value string) string {
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	}
	return value
}
