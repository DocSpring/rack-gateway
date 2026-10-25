package slack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// Audit actions of a command run in a process: an exec starting and ending, and a process starting (`run`).
var (
	actionProcessStart     = audit.BuildAction(rbac.ResourceProcess.String(), rbac.ActionStart.String())
	actionProcessExec      = audit.BuildAction(rbac.ResourceProcess.String(), rbac.ActionExec.String())
	actionProcessExecStart = actionProcessExec + ".start"
)

// processCommand is what a Slack message tells about a command run in a process, read from its audit entry's
// details. The command and a script's text stay in the audit log: a command line can carry a credential the
// redaction doesn't recognize, and a Slack channel reaches more people than the audit log. The message says how
// long the command is and links to the entry instead.
type processCommand struct {
	Path          string `json:"path"`
	ProcessID     string `json:"process_id"`
	Command       string `json:"command"`
	Justification string `json:"justification"`
	Script        string `json:"script"`
	ScriptName    string `json:"script_name"`
	ScriptSHA256  string `json:"script_sha256"`
}

// parseProcessCommand reads the command of a process action's audit entry. ok is false for any other entry.
func parseProcessCommand(auditLog *db.AuditLog) (processCommand, bool) {
	switch auditLog.Action {
	case actionProcessStart, actionProcessExec, actionProcessExecStart:
	default:
		return processCommand{}, false
	}
	var command processCommand
	if json.Unmarshal([]byte(auditLog.Details), &command) != nil || command.Command == "" {
		return processCommand{}, false
	}
	return command, true
}

// isAttachedRunPlaceholder reports whether auditLog is the start of an attached run's placeholder process (see
// convox.AttachedRunPlaceholder). Its command runs with the exec that follows, whose message tells about it, so
// the start isn't worth a message of its own. A refused start is: it's the only record of the run.
func isAttachedRunPlaceholder(auditLog *db.AuditLog) bool {
	command, ok := parseProcessCommand(auditLog)
	return ok && auditLog.Action == actionProcessStart && auditLog.Status == audit.StatusSuccess &&
		command.Command == convox.AttachedRunPlaceholder
}

// app is the app the command ran in, from the rack path (/apps/{app}/...).
func (p processCommand) app() string {
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "apps" {
		return parts[1]
	}
	return ""
}

// where says where the command ran, as escaped mrkdwn: an exec's process, or a run's service.
func (p processCommand) where(action string) string {
	if action != actionProcessStart {
		return "in process " + inlineCode(p.ProcessID)
	}
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	if len(parts) >= 4 && parts[2] == "services" {
		return "in a new " + inlineCode(parts[3]) + " process"
	}
	return "in a new process"
}

// size describes the command's length and its script, without their text.
func (p processCommand) size() string {
	text := "Command: " + strconv.Itoa(len(p.Command)) + " bytes"
	if p.ScriptName != "" {
		lines := scriptrun.Script{Text: p.Script}.Lines()
		text += fmt.Sprintf(" · Script: %s, %d lines, %d bytes", escapeMrkdwn(p.ScriptName), lines, len(p.Script))
		if p.ScriptSHA256 != "" {
			text += ", SHA-256 " + escapeMrkdwn(p.ScriptSHA256)
		}
	}
	return text
}

// formatProcessMessage formats a command run in a process: who ran it, where, its justification, how long it is
// and a link to its audit entry, which holds the command.
func (n *Notifier) formatProcessMessage(
	auditLog *db.AuditLog, command processCommand,
) (string, []map[string]interface{}) {
	emoji := "▶️"
	if isFailureStatus(auditLog.Status) {
		emoji = "❌"
	}
	user := formatUserIdentifier(auditLog)
	title := fmt.Sprintf("%s *%s* %s", emoji, escapeMrkdwn(auditLog.Action), command.where(auditLog.Action))
	justification := "_none given_"
	if command.Justification != "" {
		justification = truncateMrkdwn(escapeMrkdwn(command.Justification), maxSectionValue)
	}
	blocks := []map[string]interface{}{
		mrkdwnSection(title),
		{
			"type": "section",
			"fields": []map[string]interface{}{
				mrkdwnField("User", user),
				mrkdwnField("App", escapeMrkdwn(command.app())),
				mrkdwnField("Status", escapeMrkdwn(auditLog.Status)),
				mrkdwnField("Time", auditLog.Timestamp.Format(time.RFC3339)),
			},
		},
		mrkdwnSection("*Justification:*\n" + justification),
		{"type": "context", "elements": []map[string]interface{}{mrkdwnElement(command.size())}},
	}
	if link := n.auditEntryLink(auditLog.ID); link != "" {
		blocks = append(blocks, mrkdwnSection(link))
	}
	blocks = appendContextBlock(blocks, auditLog)
	blocks = append(blocks, map[string]interface{}{"type": "divider"})
	return identify(n.gateway, title+" - "+user, blocks)
}

// auditEntryLink links to the audit entry in the web UI, whose audit log page opens it (?entry=<id>), or is ""
// when the gateway has no URL.
func (n *Notifier) auditEntryLink(id int64) string {
	if n.gateway.URL == "" || id == 0 {
		return ""
	}
	target := n.gateway.URL + "/app/audit-logs?entry=" + strconv.FormatInt(id, 10)
	return mrkdwnLink(target, "View the command in the audit log")
}
