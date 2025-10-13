package slack

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/getsentry/sentry-go"
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
			fmt.Printf("Failed to send Slack notification to channel %s: %v\n", channelID, err)
			// Capture in Sentry
			sentry.CaptureException(err)
		}
	}

	return nil
}

// matchActionToChannels returns channel IDs that match the given action
func (n *Notifier) matchActionToChannels(action string, channelActions map[string]interface{}) []string {
	var matchedChannels []string

	for _, channelData := range channelActions {
		channelMap, ok := channelData.(map[string]interface{})
		if !ok {
			continue
		}

		channelID, ok := channelMap["id"].(string)
		if !ok || channelID == "" {
			continue
		}

		actions, ok := channelMap["actions"].([]interface{})
		if !ok {
			continue
		}

		// Check if any pattern matches the action
		for _, patternInterface := range actions {
			pattern, ok := patternInterface.(string)
			if !ok {
				continue
			}

			if matchGlob(pattern, action) {
				matchedChannels = append(matchedChannels, channelID)
				break
			}
		}
	}

	return matchedChannels
}

// matchGlob performs simple glob pattern matching
func matchGlob(pattern, text string) bool {
	matched, _ := filepath.Match(pattern, text)
	return matched
}

// formatAuditLogMessage formats an audit log into a Slack message
func (n *Notifier) formatAuditLogMessage(auditLog *db.AuditLog) (string, []map[string]interface{}) {
	// Determine emoji based on action type and status
	emoji := "📝"

	if strings.HasPrefix(auditLog.Action, audit.ActionScopeMFAMethod+".") ||
		strings.HasPrefix(auditLog.Action, audit.ActionScopeMFAPreferences+".") ||
		strings.HasPrefix(auditLog.Action, audit.ActionScopeMFAVerification+".") ||
		strings.HasPrefix(auditLog.Action, audit.ActionScopeMFABackupCodes+".") ||
		strings.HasPrefix(auditLog.Action, audit.ActionScopeTrustedDevice+".") {
		emoji = "🔐"
	} else if strings.HasPrefix(auditLog.Action, audit.ActionScopeLogin+".") || strings.HasPrefix(auditLog.Action, audit.ActionScopeLogout+".") {
		emoji = "🔑"
		if auditLog.Status != audit.StatusSuccess {
			emoji = "🚨"
		}
	} else if strings.HasPrefix(auditLog.Action, rbac.ResourceStringDeployApprovalRequest+".") {
		emoji = "🚀"
	} else if strings.HasPrefix(auditLog.Action, rbac.ResourceStringAPIToken+".") {
		emoji = "🔑"
	} else if strings.HasPrefix(auditLog.Action, "user.role.") {
		emoji = "👤"
	}

	if auditLog.Status == audit.StatusDenied || auditLog.Status == audit.StatusError || auditLog.Status == audit.StatusFailed {
		if emoji == "📝" {
			emoji = "❌"
		}
	}

	// Build text (fallback for notifications)
	user := auditLog.UserEmail
	if auditLog.UserName != "" {
		user = fmt.Sprintf("%s (%s)", auditLog.UserName, auditLog.UserEmail)
	}
	if auditLog.APITokenName != "" {
		user = fmt.Sprintf("API Token: %s", auditLog.APITokenName)
	}

	text := fmt.Sprintf("%s *%s* - %s", emoji, auditLog.Action, user)

	// Build rich blocks
	blocks := []map[string]interface{}{
		{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*%s %s*", emoji, auditLog.Action),
			},
		},
		{
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
		},
	}

	// Add resource fields if present
	if auditLog.Resource != "" || auditLog.ResourceType != "" {
		fields := blocks[1]["fields"].([]map[string]interface{})
		var newFields []map[string]interface{}

		if auditLog.Resource != "" {
			resourceText := auditLog.Resource
			if auditLog.ResourceType != "" {
				resourceText = fmt.Sprintf("%s (%s)", auditLog.Resource, auditLog.ResourceType)
			}
			newFields = append(newFields, map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Resource:*\n%s", resourceText),
			})
		}

		// Insert resource field after user, before status
		blocks[1]["fields"] = append(fields[:1], append(newFields, fields[1:]...)...)
	}

	// Add details if present
	if auditLog.Details != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Details:*\n```%s```", auditLog.Details),
			},
		})
	}

	// Add context (IP, user agent)
	contextElements := []map[string]interface{}{}
	if auditLog.IPAddress != "" {
		contextElements = append(contextElements, map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("IP: %s", auditLog.IPAddress),
		})
	}
	if len(contextElements) > 0 {
		blocks = append(blocks, map[string]interface{}{
			"type":     "context",
			"elements": contextElements,
		})
	}

	// Add divider
	blocks = append(blocks, map[string]interface{}{
		"type": "divider",
	})

	return text, blocks
}
