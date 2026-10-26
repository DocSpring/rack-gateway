package slack

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// processEntry is the audit entry of a process action with details.
func processEntry(t *testing.T, action, status string, details map[string]string) *db.AuditLog {
	t.Helper()
	data, err := json.Marshal(details)
	require.NoError(t, err)
	return &db.AuditLog{
		ID: 42, Action: action, UserEmail: "dev@example.com", UserName: "Dev", Status: status,
		Timestamp: time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC), Details: string(data), IPAddress: "10.0.0.7",
	}
}

func messageText(t *testing.T, text string, blocks []map[string]interface{}) string {
	t.Helper()
	blocksJSON, err := json.Marshal(blocks)
	require.NoError(t, err)
	return text + "\n" + string(blocksJSON)
}

// A command's message says who ran it, where, why, how long it is and where its audit entry is. The command and
// its script stay in the audit log: a credential the redaction doesn't recognize mustn't reach Slack.
func TestProcessMessageLeavesOutTheCommand(t *testing.T) {
	const unredacted = "tok_9f8e7d6c5b4a"
	script := "client = Api.new('" + unredacted + "')\nputs client.accounts.count\n"
	entry := processEntry(t, actionProcessExecStart, audit.StatusSuccess, map[string]string{
		"path":          "/apps/myapp/processes/web-6f8b9c-x2kq4/exec",
		"process_id":    "web-6f8b9c-x2kq4",
		"command":       "rails runner count.rb --token " + unredacted,
		"script":        script,
		"script_name":   "count.rb",
		"script_sha256": strings.Repeat("ab", 32),
		"justification": "Count the accounts on the <pro> plan for the pricing review",
	})

	text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(entry)
	message := messageText(t, text, blocks)
	require.Contains(t, message, "process.exec.start* in process `web-6f8b9c-x2kq4`")
	require.Contains(t, message, "Dev (dev@example.com)")
	require.Contains(t, message, "*App:*\\nmyapp")
	require.Contains(t, message, "Count the accounts on the \\u0026lt;pro\\u0026gt; plan for the pricing review")
	require.Contains(t, message, "Command: 46 bytes · Script: count.rb, 2 lines, 64 bytes, SHA-256 "+
		strings.Repeat("ab", 32))
	require.Contains(t, message, "\\u003chttps://gateway.example.com/app/audit-logs?entry=42|View the command")
	for _, hidden := range []string{unredacted, "rails runner", "client.accounts"} {
		require.NotContains(t, message, hidden)
	}
	requireFitsSlackLimits(t, text, blocks)
}

// A run's message names its service. A refused command without a justification says so.
func TestProcessMessageForARun(t *testing.T) {
	entry := processEntry(t, actionProcessStart, audit.StatusDenied, map[string]string{
		"path":    "/apps/myapp/services/worker/processes",
		"command": "bin/rake reports:send",
	})

	text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(entry)
	message := messageText(t, text, blocks)
	require.Contains(t, message, "❌ *process.start* in a new `worker` process")
	require.Contains(t, message, "*Justification:*\\n_none given_")
	require.Contains(t, message, "Command: 21 bytes")
	require.NotContains(t, message, "reports:send")
}

// The longest justification and a long script name still fit Slack's limits.
func TestLargestProcessMessageFitsSlackLimits(t *testing.T) {
	entry := processEntry(t, actionProcessExec, audit.StatusError, map[string]string{
		"path":          "/apps/" + longValue + "/processes/" + longValue + "/exec",
		"process_id":    longValue,
		"command":       longValue,
		"justification": strings.Repeat("&<>`", audit.MaxJustificationChars/4),
		"script":        longValue,
		"script_name":   strings.Repeat("a", 64),
	})
	entry.UserName, entry.IPAddress = longValue, longValue

	text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(entry)
	requireFitsSlackLimits(t, text, blocks)
}

// An attached run starts a placeholder process, then execs its command in it. The exec's message tells about the
// command, so the placeholder's start isn't posted, unless it was refused: then it's the run's only record.
func TestAttachedRunPlaceholderIsntPosted(t *testing.T) {
	details := map[string]string{"path": "/apps/myapp/services/web/processes", "command": convox.AttachedRunPlaceholder}
	require.True(t, isAttachedRunPlaceholder(processEntry(t, actionProcessStart, audit.StatusSuccess, details)))
	require.False(t, isAttachedRunPlaceholder(processEntry(t, actionProcessStart, audit.StatusDenied, details)))

	details["command"] = "sleep 3600 && echo done"
	require.False(t, isAttachedRunPlaceholder(processEntry(t, actionProcessStart, audit.StatusSuccess, details)))
	details["command"] = convox.AttachedRunPlaceholder
	require.False(t, isAttachedRunPlaceholder(processEntry(t, actionProcessExecStart, audit.StatusSuccess, details)))
}

// Other entries, and a process entry without a command, keep the usual format.
func TestOnlyProcessCommandsGetTheProcessFormat(t *testing.T) {
	_, ok := parseProcessCommand(processEntry(t, "env.set", audit.StatusSuccess, map[string]string{"command": "x"}))
	require.False(t, ok)
	_, ok = parseProcessCommand(processEntry(t, actionProcessExec, audit.StatusSuccess, map[string]string{}))
	require.False(t, ok)
	_, ok = parseProcessCommand(&db.AuditLog{Action: actionProcessStart, Details: "not json"})
	require.False(t, ok)
}

// A command's message says which env vars it got, and warns when it was given the real values of the read-only ones.
// An entry from before the gateway recorded them says nothing about them.
func TestProcessMessageSaysWhichEnvVarsTheCommandGot(t *testing.T) {
	for want, env := range map[string]map[string]interface{}{
		"*Env vars:*\\nthe 2 in the app's command_env_vars; read-only DATABASE_URL (from READ_ONLY": {
			"env_vars":      []string{"DATABASE_URL", "PATH"},
			"env_read_only": map[string]string{"DATABASE_URL": "READ_ONLY_DATABASE_URL"},
		},
		"*Env vars:*\\nthe 2 in the app's command_env_vars, plus SENTRY_DSN": {
			"env_vars": []string{"DATABASE_URL", "PATH", "SENTRY_DSN"}, "env_vars_added": []string{"SENTRY_DSN"},
		},
		"*Env vars:*\\n:warning: *Can change data:* run with --write": {
			"env_vars": []string{"DATABASE_URL", "PATH"}, "write": true,
		},
		"*Env vars:*\\nevery env var except RACK_URL": {"all_env_vars": true},
		"*Env vars:*\\nno env vars":                   {"env_vars": []string{}},
	} {
		details := map[string]interface{}{
			"path": "/apps/myapp/processes/p1/exec", "process_id": "p1", "command": "rake",
		}
		for key, value := range env {
			details[key] = value
		}
		data, err := json.Marshal(details)
		require.NoError(t, err)
		entry := processEntry(t, actionProcessExecStart, audit.StatusSuccess, nil)
		entry.Details = string(data)

		text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(entry)
		require.Contains(t, messageText(t, text, blocks), want)
	}

	entry := processEntry(t, actionProcessExecStart, audit.StatusSuccess, map[string]string{
		"path": "/apps/myapp/processes/p1/exec", "process_id": "p1", "command": "rake",
	})
	text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(entry)
	require.NotContains(t, messageText(t, text, blocks), "Env vars")
}
