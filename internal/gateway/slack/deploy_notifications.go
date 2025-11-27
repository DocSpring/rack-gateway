package slack

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

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
func (_ *Notifier) formatDeployApprovalAlert(
	req *db.DeployApprovalRequest,
	gatewayDomain string,
) (string, []map[string]interface{}) {
	branchText := deployApprovalBranch(req)
	text := deployApprovalFallbackText(branchText, req.Message)

	blocks := []map[string]interface{}{
		deployApprovalHeaderBlock(branchText),
		deployApprovalDetailsBlock(req),
	}

	if messageBlock := deployApprovalMessageBlock(req); messageBlock != nil {
		blocks = append(blocks, messageBlock)
	}

	blocks = append(blocks, deployApprovalLinksBlock(req, gatewayDomain))

	if contextBlock := deployApprovalContextBlock(req); contextBlock != nil {
		blocks = append(blocks, contextBlock)
	}

	blocks = append(blocks, slackDividerBlock())

	return text, blocks
}

func deployApprovalBranch(req *db.DeployApprovalRequest) string {
	if req.GitBranch == "" {
		return "unknown branch"
	}

	return req.GitBranch
}

func deployApprovalFallbackText(branch, message string) string {
	return fmt.Sprintf("🚀 Deploy approval needed for %s - %s", branch, message)
}

func deployApprovalHeaderBlock(branch string) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("*🚀 Deploy approval needed for `%s`*", branch),
		},
	}
}

func deployApprovalDetailsBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"fields": []map[string]interface{}{
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*App:*\n%s", req.App),
			},
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Commit:*\n`%s`", shortCommitHash(req.GitCommitHash)),
			},
		},
	}
}

func deployApprovalMessageBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	if req.Message == "" {
		return nil
	}

	return map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("*Message:*\n%s", req.Message),
		},
	}
}

func deployApprovalLinksBlock(req *db.DeployApprovalRequest, gatewayDomain string) map[string]interface{} {
	elements := []map[string]interface{}{
		{
			"type": "mrkdwn",
			"text": fmt.Sprintf(
				"🔗 <https://%s/app/deploy-approval-requests/%s|View Approval Request>",
				gatewayDomain,
				req.PublicID,
			),
		},
	}

	if req.PrURL != "" {
		elements = append(elements, map[string]interface{}{
			"type": "mrkdwn",
			"text": fmt.Sprintf("🔗 <%s|GitHub PR>", req.PrURL),
		})
	}

	if ciElement := deployApprovalCIElement(req.CIMetadata); ciElement != nil {
		elements = append(elements, ciElement)
	}

	return map[string]interface{}{
		"type":   "section",
		"fields": elements,
	}
}

func deployApprovalCIElement(metadata json.RawMessage) map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}

	var ciMeta map[string]interface{}
	if err := json.Unmarshal(metadata, &ciMeta); err != nil {
		return nil
	}

	buildURL, ok := ciMeta["build_url"].(string)
	if !ok || buildURL == "" {
		return nil
	}

	return map[string]interface{}{
		"type": "mrkdwn",
		"text": fmt.Sprintf("🔗 <%s|CI Pipeline>", buildURL),
	}
}

func deployApprovalContextBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	contextText := deployApprovalCreatorText(req)
	if contextText == "" {
		return nil
	}

	return map[string]interface{}{
		"type": "context",
		"elements": []map[string]interface{}{
			{
				"type": "mrkdwn",
				"text": contextText,
			},
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf(
					"<!date^%d^{date_short_pretty} at {time}|%s>",
					req.CreatedAt.Unix(),
					req.CreatedAt.Format(time.RFC3339),
				),
			},
		},
	}
}

func deployApprovalCreatorText(req *db.DeployApprovalRequest) string {
	switch {
	case req.CreatedByEmail != "" && req.CreatedByName != "":
		return fmt.Sprintf("Created by %s (%s)", req.CreatedByName, req.CreatedByEmail)
	case req.CreatedByEmail != "":
		return fmt.Sprintf("Created by %s", req.CreatedByEmail)
	case req.CreatedByAPITokenName != "":
		return fmt.Sprintf("Created by API token: %s", req.CreatedByAPITokenName)
	default:
		return ""
	}
}

func slackDividerBlock() map[string]interface{} {
	return map[string]interface{}{
		"type": "divider",
	}
}

func shortCommitHash(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}
