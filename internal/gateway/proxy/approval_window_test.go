package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// expireApproval moves the harness's approval past its window, without the expiry sweep marking it.
func (e *approvalExecHarness) expireApproval(t *testing.T) {
	t.Helper()
	_, err := e.database.DB().Exec(
		`UPDATE deploy_approval_requests SET approval_expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`,
		e.approval.ID,
	)
	require.NoError(t, err)
}

// An exec goes on its approval's record before it runs, so the record has to find the approval still open: one
// whose window has passed since it was looked up runs nothing.
func TestApprovalExecIsRefusedOnceTheApprovalExpired(t *testing.T) {
	e := newApprovalExecHarness(t)
	e.expireApproval(t)
	require.Equal(t, http.StatusForbidden, e.exec("bin/pre_release_checks migrate"))
}

// A process start's approval is checked again once the whole request has been read: a request sent slowly can't
// carry the approval past its window.
func TestApprovalProcessStartIsCheckedAgainBeforeItsForwarded(t *testing.T) {
	e := newApprovalExecHarness(t)
	start := func() error {
		r := httptest.NewRequest(http.MethodPost, "/apps/docspring/services/web/processes", nil)
		r.Header.Set("Command", "bin/pre_release_checks migrate")
		tracker := &deployApprovalTracker{request: e.approval, app: "docspring"}
		r = r.WithContext(context.WithValue(r.Context(), deployApprovalContextKey, tracker))
		return e.h.validateProcessCommand(r, r.URL.Path)
	}
	require.NoError(t, start())

	e.expireApproval(t)
	err := start()
	var refusal *refusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, http.StatusForbidden, refusal.status)
	require.ErrorContains(t, err, "is no longer approved")
}

// A process start takes no body at all, not even whitespace: the gateway reads the request before it forwards it,
// so a body could only hold options a later rack might read, or slow the request down.
func TestProcessStartRefusesAnyBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/apps/docspring/services/web/processes", nil)
	for _, body := range []string{" ", "\n", "command=ls"} {
		err := checkHeaderOnlyRequest(r, r.URL.Path, []byte(body))
		require.ErrorContains(t, err, "this request takes no body", "body %q", body)
	}
	require.NoError(t, checkHeaderOnlyRequest(r, r.URL.Path, nil))
}

// Every exec on a process stays on the approval's record, in order, and a value an earlier release stored as a
// single string becomes the list's first command.
func TestApprovalRecordsEveryExecOnAProcess(t *testing.T) {
	e := newApprovalExecHarness(t)
	app := "docspring"
	commands := []string{"bin/pre_release_checks migrate", "bin/pre_release_checks seed"}
	require.NoError(t, e.database.UpsertSetting(&app, "approved_deploy_commands", commands, nil))
	_, err := e.database.DB().Exec(
		`UPDATE deploy_approval_requests SET exec_commands = '{"p1": "bin/earlier"}'::jsonb WHERE id = $1`,
		e.approval.ID,
	)
	require.NoError(t, err)

	for _, command := range commands {
		require.Zero(t, e.exec(command))
	}

	recorded, err := e.database.GetDeployApprovalRequest(e.approval.ID)
	require.NoError(t, err)
	var execs map[string][]string
	require.NoError(t, json.Unmarshal(recorded.ExecCommands, &execs))
	require.Equal(t, map[string][]string{"p1": append([]string{"bin/earlier"}, commands...)}, execs)
}
