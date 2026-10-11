package slack

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// mrkdwnEscaper escapes the characters Slack's mrkdwn reserves for links, mentions and formatting.
var mrkdwnEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// testMessageText is the text of the message the Integrations page's Test button sends.
const testMessageText = "🧪 Test notification"

// TestMessage is the message the Integrations page's Test button sends to a channel. It names the gateway like
// every other message.
func TestMessage(gateway config.GatewayIdentity) (string, []map[string]interface{}) {
	return identify(gateway, testMessageText, []map[string]interface{}{mrkdwnSection(testMessageText)})
}

// identify names the gateway in a message, since one Slack workspace can get messages from several gateways:
// the fallback text, which Slack shows in notifications, starts with the gateway's label as email subjects do
// ("Rack Gateway (Staging): ..."), and the first block is that label, linked to the gateway.
func identify(
	gateway config.GatewayIdentity,
	text string,
	blocks []map[string]interface{},
) (string, []map[string]interface{}) {
	label := mrkdwnEscaper.Replace(gateway.Label())
	return label + ": " + text, append([]map[string]interface{}{gatewayBlock(gateway, label)}, blocks...)
}

// gatewayBlock is a section naming the gateway in bold, linked to the gateway when it has a URL.
func gatewayBlock(gateway config.GatewayIdentity, label string) map[string]interface{} {
	if gateway.URL != "" {
		label = fmt.Sprintf("<%s|%s>", mrkdwnEscaper.Replace(gateway.URL), label)
	}
	return mrkdwnSection("*" + label + "*")
}

// mrkdwnSection is a section block of mrkdwn text.
func mrkdwnSection(text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": text,
		},
	}
}
