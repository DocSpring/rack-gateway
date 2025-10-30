package slack

import (
	"encoding/base64"
	"encoding/json"
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
	} else if strings.HasPrefix(auditLog.Action, rbac.ResourceDeployApprovalRequest.String()+".") {
		emoji = "🚀"
	} else if strings.HasPrefix(auditLog.Action, rbac.ResourceAPIToken.String()+".") {
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

// NotifyDeployApprovalCreated sends a rich notification for a new deploy approval request
func (n *Notifier) NotifyDeployApprovalCreated(req *db.DeployApprovalRequest, gatewayDomain string) error {
	// Get Slack integration
	integration, err := n.database.GetSlackIntegration()
	if err != nil || integration == nil {
		// No integration configured, silently skip
		return nil
	}

	// Check if deploy approval alerts are enabled
	if !integration.AlertDeployApprovalsEnabled {
		return nil
	}

	// Check if channel is configured
	channelID := integration.AlertDeployApprovalsChannelID
	if channelID == "" {
		return nil
	}

	// Decrypt bot token
	botToken, err := base64.StdEncoding.DecodeString(integration.BotTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt bot token: %w", err)
	}

	// Create Slack client
	client := NewClient(string(botToken))

	// Format rich message
	text, blocks := n.formatDeployApprovalAlert(req, gatewayDomain)

	// Send to configured channel
	if err := client.PostMessage(channelID, text, blocks); err != nil {
		gtwlog.Errorf("slack notifier: failed to send deploy approval alert to channel %s: %v", channelID, err)
		sentry.CaptureException(err)
		return err
	}

	return nil
}

// formatDeployApprovalAlert formats a deploy approval request into a rich Slack message
func (n *Notifier) formatDeployApprovalAlert(req *db.DeployApprovalRequest, gatewayDomain string) (string, []map[string]interface{}) {
	// Build text (fallback for notifications)
	branchText := req.GitBranch
	if branchText == "" {
		branchText = "unknown branch"
	}
	text := fmt.Sprintf("🚀 Deploy approval needed for %s - %s", branchText, req.Message)

	// Build rich blocks
	blocks := []map[string]interface{}{
		{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*🚀 Deploy approval needed for `%s`*", branchText),
			},
		},
		{
			"type": "section",
			"fields": []map[string]interface{}{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*App:*\n%s", req.App),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Commit:*\n`%s`", req.GitCommitHash[:7]),
				},
			},
		},
	}

	// Add message if present
	if req.Message != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Message:*\n%s", req.Message),
			},
		})
	}

	// Build links section
	linksElements := []map[string]interface{}{
		{
			"type": "mrkdwn",
			"text": fmt.Sprintf("🔗 <https://%s/app/deploy-approvals/%s|View Approval Request>", gatewayDomain, req.PublicID),
		},
	}

	// Add PR link if available
	if req.PrURL != "" {
		linksElements = append(linksElements, map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("🔗 <%s|GitHub PR>", req.PrURL),
		})
	}

	// Extract CI pipeline URL from ci_metadata if available
	if len(req.CIMetadata) > 0 {
		var ciMeta map[string]interface{}
		if err := json.Unmarshal(req.CIMetadata, &ciMeta); err == nil {
			// Try to extract CircleCI URL
			if buildURL, ok := ciMeta["build_url"].(string); ok && buildURL != "" {
				linksElements = append(linksElements, map[string]interface{}{
					"type": "mrkdwn",
					"text": fmt.Sprintf("🔗 <%s|CI Pipeline>", buildURL),
				})
			}
		}
	}

	blocks = append(blocks, map[string]interface{}{
		"type":   "section",
		"fields": linksElements,
	})

	// Add creator context
	creatorText := ""
	if req.CreatedByEmail != "" {
		creatorText = fmt.Sprintf("Created by %s", req.CreatedByEmail)
		if req.CreatedByName != "" {
			creatorText = fmt.Sprintf("Created by %s (%s)", req.CreatedByName, req.CreatedByEmail)
		}
	} else if req.CreatedByAPITokenName != "" {
		creatorText = fmt.Sprintf("Created by API token: %s", req.CreatedByAPITokenName)
	}

	if creatorText != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "context",
			"elements": []map[string]interface{}{
				{
					"type": "mrkdwn",
					"text": creatorText,
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("<!date^%d^{date_short_pretty} at {time}|%s>", req.CreatedAt.Unix(), req.CreatedAt.Format(time.RFC3339)),
				},
			},
		})
	}

	// Add divider
	blocks = append(blocks, map[string]interface{}{
		"type": "divider",
	})

	return text, blocks
}
