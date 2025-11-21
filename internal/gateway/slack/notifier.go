package slack

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Notifier sends audit log events to Slack
type Notifier struct {
	database *db.Database
}

// NewNotifier creates a new Slack notifier
func NewNotifier(database *db.Database) *Notifier {
	return &Notifier{
		database: database,
	}
}

// NotifyAuditEvent sends an audit log event to Slack if configured
func (n *Notifier) NotifyAuditEvent(auditLog *db.AuditLog) error {
	// Get Slack integration
	integration, err := n.database.GetSlackIntegration()
	if err != nil || integration == nil {
		// No integration configured, silently skip
		return nil
	}

	// Match action to channels using glob patterns
	channels := n.matchActionToChannels(auditLog.Action, integration.ChannelActions)
	if len(channels) == 0 {
		// No matching channels for this action
		return nil
	}

	// Decrypt bot token
	botToken, err := base64.StdEncoding.DecodeString(integration.BotTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt bot token: %w", err)
	}

	// Create Slack client
	client := NewClient(string(botToken))

	// Format message
	text, blocks := n.formatAuditLogMessage(auditLog)

	// Send to all matching channels
	for _, channelID := range channels {
		if err := client.PostMessage(channelID, text, blocks); err != nil {
			// Log error but continue sending to other channels
			gtwlog.Errorf("slack notifier: failed to send notification to channel %s: %v", channelID, err)
			// Capture in Sentry
			sentry.CaptureException(err)
		}
	}

	return nil
}

// matchActionToChannels returns channel IDs that match the given action
func (_ *Notifier) matchActionToChannels(action string, channelActions map[string]interface{}) []string {
	var matchedChannels []string

	for _, channelData := range channelActions {
		if channelID := extractMatchingChannel(channelData, action); channelID != "" {
			matchedChannels = append(matchedChannels, channelID)
		}
	}

	return matchedChannels
}

// extractMatchingChannel extracts channel ID if any action pattern matches
func extractMatchingChannel(channelData interface{}, action string) string {
	channelMap, ok := channelData.(map[string]interface{})
	if !ok {
		return ""
	}

	channelID, ok := channelMap["id"].(string)
	if !ok || channelID == "" {
		return ""
	}

	actions, ok := channelMap["actions"].([]interface{})
	if !ok {
		return ""
	}

	if matchesAnyPattern(actions, action) {
		return channelID
	}

	return ""
}

// matchesAnyPattern checks if action matches any of the patterns
func matchesAnyPattern(patterns []interface{}, action string) bool {
	for _, patternInterface := range patterns {
		pattern, ok := patternInterface.(string)
		if !ok {
			continue
		}

		if matchGlob(pattern, action) {
			return true
		}
	}
	return false
}

// matchGlob performs simple glob pattern matching
func matchGlob(pattern, text string) bool {
	matched, _ := filepath.Match(pattern, text)
	return matched
}

// formatAuditLogMessage formats an audit log into a Slack message
func (_ *Notifier) formatAuditLogMessage(auditLog *db.AuditLog) (string, []map[string]interface{}) {
	emoji := determineEmojiForAction(auditLog)
	user := formatUserIdentifier(auditLog)
	text := fmt.Sprintf("%s *%s* - %s", emoji, auditLog.Action, user)

	blocks := buildSlackBlocks(auditLog, emoji, user)

	return text, blocks
}

// determineEmojiForAction selects appropriate emoji based on action type and status
func determineEmojiForAction(auditLog *db.AuditLog) string {
	emoji := selectEmojiByActionPrefix(auditLog.Action)

	// Override for login/logout with non-success status
	if isLoginOrLogout(auditLog.Action) && auditLog.Status != audit.StatusSuccess {
		emoji = "🚨"
	}

	// Override for failed/denied/error status with default emoji
	if isFailureStatus(auditLog.Status) && emoji == "📝" {
		emoji = "❌"
	}

	return emoji
}

// selectEmojiByActionPrefix returns emoji based on action prefix
func selectEmojiByActionPrefix(action string) string {
	switch {
	case isMFAAction(action):
		return "🔐"
	case isLoginOrLogout(action):
		return "🔑"
	case strings.HasPrefix(action, rbac.ResourceDeployApprovalRequest.String()+"."):
		return "🚀"
	case strings.HasPrefix(action, rbac.ResourceAPIToken.String()+"."):
		return "🔑"
	case strings.HasPrefix(action, "user.role."):
		return "👤"
	default:
		return "📝"
	}
}

// isMFAAction checks if action is MFA-related
func isMFAAction(action string) bool {
	mfaScopes := []string{
		audit.ActionScopeMFAMethod,
		audit.ActionScopeMFAPreferences,
		audit.ActionScopeMFAVerification,
		audit.ActionScopeMFABackupCodes,
		audit.ActionScopeTrustedDevice,
	}

	for _, scope := range mfaScopes {
		if strings.HasPrefix(action, scope+".") {
			return true
		}
	}
	return false
}

// isLoginOrLogout checks if action is login or logout related
func isLoginOrLogout(action string) bool {
	return strings.HasPrefix(action, audit.ActionScopeLogin+".") ||
		strings.HasPrefix(action, audit.ActionScopeLogout+".")
}

// isFailureStatus checks if status indicates failure
func isFailureStatus(status string) bool {
	return status == audit.StatusDenied || status == audit.StatusError || status == audit.StatusFailed
}

// formatUserIdentifier formats user identification string
func formatUserIdentifier(auditLog *db.AuditLog) string {
	if auditLog.APITokenName != "" {
		return fmt.Sprintf("API Token: %s", auditLog.APITokenName)
	}
	if auditLog.UserName != "" {
		return fmt.Sprintf("%s (%s)", auditLog.UserName, auditLog.UserEmail)
	}
	return auditLog.UserEmail
}

// buildSlackBlocks constructs the full Slack message block structure
func buildSlackBlocks(auditLog *db.AuditLog, emoji, user string) []map[string]interface{} {
	blocks := []map[string]interface{}{
		{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*%s %s*", emoji, auditLog.Action),
			},
		},
		buildFieldsBlock(auditLog, user),
	}

	blocks = appendResourceBlock(blocks, auditLog)
	blocks = appendDetailsBlock(blocks, auditLog)
	blocks = appendContextBlock(blocks, auditLog)
	blocks = append(blocks, map[string]interface{}{"type": "divider"})

	return blocks
}

// buildFieldsBlock creates the fields section with user, status, and time
func buildFieldsBlock(auditLog *db.AuditLog, user string) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"fields": []map[string]interface{}{
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*User:*\n%s", user),
			},
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Status:*\n%s", auditLog.Status),
			},
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Time:*\n%s", auditLog.Timestamp.Format(time.RFC3339)),
			},
		},
	}
}

// appendResourceBlock adds resource information if present
func appendResourceBlock(blocks []map[string]interface{}, auditLog *db.AuditLog) []map[string]interface{} {
	if auditLog.Resource == "" && auditLog.ResourceType == "" {
		return blocks
	}

	fields := blocks[1]["fields"].([]map[string]interface{})
	resourceText := auditLog.Resource
	if auditLog.ResourceType != "" && auditLog.Resource != "" {
		resourceText = fmt.Sprintf("%s (%s)", auditLog.Resource, auditLog.ResourceType)
	}

	newField := map[string]interface{}{
		"type": "mrkdwn",
		"text": fmt.Sprintf("*Resource:*\n%s", resourceText),
	}

	// Insert resource field after user, before status
	blocks[1]["fields"] = append(fields[:1], append([]map[string]interface{}{newField}, fields[1:]...)...)

	return blocks
}

// appendDetailsBlock adds details section if present
func appendDetailsBlock(blocks []map[string]interface{}, auditLog *db.AuditLog) []map[string]interface{} {
	if auditLog.Details == "" {
		return blocks
	}

	return append(blocks, map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("*Details:*\n```%s```", auditLog.Details),
		},
	})
}

// appendContextBlock adds context section with IP if present
func appendContextBlock(blocks []map[string]interface{}, auditLog *db.AuditLog) []map[string]interface{} {
	if auditLog.IPAddress == "" {
		return blocks
	}

	return append(blocks, map[string]interface{}{
		"type": "context",
		"elements": []map[string]interface{}{
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("IP: %s", auditLog.IPAddress),
			},
		},
	})
}
