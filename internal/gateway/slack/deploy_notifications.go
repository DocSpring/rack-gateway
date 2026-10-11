package slack

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// NotifyDeployApprovalCreated sends a rich notification for a new deploy approval request
func (n *Notifier) NotifyDeployApprovalCreated(req *db.DeployApprovalRequest) error {
	// Get Slack integration
	integration, err := n.database.GetSlackIntegration()
	if err != nil {
		return fmt.Errorf("slack notifier: load integration: %w", err)
	}
	if integration == nil {
		return nil // no integration configured
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

	client := botClient(integration)
	if client == nil {
		return nil
	}

	// Format rich message
	text, blocks := n.formatDeployApprovalAlert(req)

	// Send to configured channel
	if err := client.PostMessage(channelID, text, blocks); err != nil {
		gtwlog.Errorf("slack notifier: failed to send deploy approval alert to channel %s: %v", channelID, err)
		sentry.CaptureException(err)
		return err
	}

	return nil
}

// formatDeployApprovalAlert formats a deploy approval request into a rich Slack message that names the gateway.
// Everything in the request except the gateway's own link comes from CI or a user, so each value is escaped
// where it's formatted.
func (n *Notifier) formatDeployApprovalAlert(req *db.DeployApprovalRequest) (string, []map[string]interface{}) {
	branch := deployApprovalBranch(req)
	text := deployApprovalFallbackText(branch, req.Message)

	blocks := []map[string]interface{}{
		deployApprovalHeaderBlock(branch),
		deployApprovalDetailsBlock(req),
	}

	if messageBlock := deployApprovalMessageBlock(req); messageBlock != nil {
		blocks = append(blocks, messageBlock)
	}

	blocks = append(blocks, deployApprovalLinksBlock(req, n.gateway.URL))

	if contextBlock := deployApprovalContextBlock(req); contextBlock != nil {
		blocks = append(blocks, contextBlock)
	}

	blocks = append(blocks, slackDividerBlock())

	return identify(n.gateway, text, blocks)
}

func deployApprovalBranch(req *db.DeployApprovalRequest) string {
	if req.GitBranch == "" {
		return "unknown branch"
	}

	return req.GitBranch
}

func deployApprovalFallbackText(branch, message string) string {
	return fmt.Sprintf("🚀 Deploy approval needed for %s - %s", escapeMrkdwn(branch), escapeMrkdwn(message))
}

func deployApprovalHeaderBlock(branch string) map[string]interface{} {
	return mrkdwnSection(fmt.Sprintf("*🚀 Deploy approval needed for %s*", inlineCode(branch)))
}

func deployApprovalDetailsBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"fields": []map[string]interface{}{
			mrkdwnElement("*App:*\n" + escapeMrkdwn(req.App)),
			mrkdwnElement("*Commit:*\n" + inlineCode(shortCommitHash(req.GitCommitHash))),
		},
	}
}

func deployApprovalMessageBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	if req.Message == "" {
		return nil
	}

	return mrkdwnSection("*Message:*\n" + escapeMrkdwn(req.Message))
}

// deployApprovalLinksBlock links to the request on this gateway, and to the pull request and CI build when their
// URLs are safe to link (see externalLink).
func deployApprovalLinksBlock(req *db.DeployApprovalRequest, gatewayURL string) map[string]interface{} {
	elements := []map[string]interface{}{
		mrkdwnElement("🔗 " + mrkdwnLink(
			gatewayURL+"/app/deploy-approval-requests/"+req.PublicID,
			"View Approval Request",
		)),
	}

	if prLink := externalLink(req.PrURL, "GitHub PR"); prLink != "" {
		elements = append(elements, mrkdwnElement("🔗 "+prLink))
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
	if !ok {
		return nil
	}

	ciLink := externalLink(buildURL, "CI Pipeline")
	if ciLink == "" {
		return nil
	}

	return mrkdwnElement("🔗 " + ciLink)
}

func deployApprovalContextBlock(req *db.DeployApprovalRequest) map[string]interface{} {
	contextText := deployApprovalCreatorText(req)
	if contextText == "" {
		return nil
	}

	return map[string]interface{}{
		"type": "context",
		"elements": []map[string]interface{}{
			mrkdwnElement(contextText),
			mrkdwnElement(fmt.Sprintf(
				"<!date^%d^{date_short_pretty} at {time}|%s>",
				req.CreatedAt.Unix(),
				req.CreatedAt.Format(time.RFC3339),
			)),
		},
	}
}

func deployApprovalCreatorText(req *db.DeployApprovalRequest) string {
	switch {
	case req.CreatedByEmail != "" && req.CreatedByName != "":
		return fmt.Sprintf("Created by %s (%s)", escapeMrkdwn(req.CreatedByName), escapeMrkdwn(req.CreatedByEmail))
	case req.CreatedByEmail != "":
		return "Created by " + escapeMrkdwn(req.CreatedByEmail)
	case req.CreatedByAPITokenName != "":
		return "Created by API token: " + escapeMrkdwn(req.CreatedByAPITokenName)
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
