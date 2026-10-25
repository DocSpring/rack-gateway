package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

const testJustification = "Count the templates of account 42 — checking a support ticket"

// processStartDetails returns the details of the admin's last audit entry for a process start whose status is
// status ("success" or "denied").
func (f *protectedEnvFixture) processStartDetails(t *testing.T, status string) map[string]interface{} {
	t.Helper()
	logs, err := f.database.GetAuditLogs(f.admin.Email, time.Time{}, 100)
	require.NoError(t, err)
	for _, entry := range logs {
		if strings.HasPrefix(entry.Action, "process.") && entry.Status == status {
			var details map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(entry.Details), &details))
			return details
		}
	}
	t.Fatalf("no %s process start in the audit log", status)
	return nil
}

func (f *protectedEnvFixture) processStarts() []http.Header {
	f.rack.mu.Lock()
	defer f.rack.mu.Unlock()
	return append([]http.Header(nil), f.rack.processStarts...)
}

func placeholderStart(justification string) map[string]string {
	headers := map[string]string{"Command": convox.AttachedRunPlaceholder}
	if justification != "" {
		headers[audit.JustificationHeader] = justification
	}
	return headers
}

// A command's justification is recorded with it, as written (the header carries it percent-encoded), and isn't
// sent on to the rack.
func TestCommandJustificationIsRecordedAndNotForwarded(t *testing.T) {
	f := newProtectedEnvFixture(t)
	headers := map[string]string{
		"Command":                 "rails runner 'puts Template.count'",
		audit.JustificationHeader: audit.EncodeJustification(testJustification),
	}
	rr := f.processRequest(t, http.MethodPost, processStartPath, f.totpCode(t), headers)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	require.Equal(t, testJustification, f.processStartDetails(t, "success")["justification"])
	starts := f.processStarts()
	require.Len(t, starts, 1)
	require.Empty(t, starts[0].Values(audit.JustificationHeader))
}

// A justification that can't say why the command is run is refused, and the refusal is audited.
func TestInvalidCommandJustificationIsRefused(t *testing.T) {
	f := newProtectedEnvFixture(t)
	for justification, reason := range map[string]string{
		audit.EncodeJustification("checking"):                            "the justification is too short",
		audit.EncodeJustification(testJustification + "\nrm"):            "must be one line",
		audit.EncodeJustification("Count templates \u202e42 tnuocca fo"): "must be one line",
	} {
		rr := f.processRequest(t, http.MethodPost, processStartPath, "", placeholderStart(justification))
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), reason)
	}
	require.Empty(t, f.processStarts())
	require.Contains(t, f.processStartDetails(t, "denied")["reason"], "the justification")
}

// An app that requires a justification refuses a command without one, `convox run`'s placeholder start included
// (so an attached run stops before its process starts), and takes one with it.
func TestRequiredCommandJustification(t *testing.T) {
	f := newProtectedEnvFixture(t)
	app := "app"
	require.NoError(t, settings.NewService(f.database).SetAppSettings(
		app, map[string]interface{}{settings.KeyRequireCommandJustification: true}, nil))

	rr := f.processRequest(t, http.MethodPost, processStartPath, "", placeholderStart(""))
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "this app requires a justification for every command")
	require.Empty(t, f.processStarts())

	justified := placeholderStart(audit.EncodeJustification(testJustification))
	rr = f.processRequest(t, http.MethodPost, processStartPath, "", justified)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Len(t, f.processStarts(), 1)
}
