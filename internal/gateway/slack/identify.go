package slack

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// testMessageText is the text of the message the Integrations page's Test button sends.
const testMessageText = "🧪 Test notification"

// TestMessage is the message the Integrations page's Test button sends to a channel. It names the gateway like
// every other message.
func TestMessage(gateway config.GatewayIdentity) (string, []map[string]interface{}) {
	return identify(gateway, testMessageText, []map[string]interface{}{mrkdwnSection(testMessageText)})
}

// identify names the gateway in a message, since one Slack workspace can get messages from several gateways:
// the fallback text, which Slack shows in notifications, starts with the gateway's label as email subjects do
// ("Rack Gateway (Staging): ..."), and the first block is that label, linked to the gateway. text is already
// mrkdwn; only the label is escaped here.
func identify(
	gateway config.GatewayIdentity,
	text string,
	blocks []map[string]interface{},
) (string, []map[string]interface{}) {
	label := escapeMrkdwn(gateway.Label())
	return label + ": " + text, append([]map[string]interface{}{gatewayBlock(gateway, label)}, blocks...)
}

// gatewayBlock is a section naming the gateway in bold, linked to the gateway when it has a URL.
func gatewayBlock(gateway config.GatewayIdentity, label string) map[string]interface{} {
	if gateway.URL != "" {
		label = mrkdwnLink(gateway.URL, label)
	}
	return mrkdwnSection("*" + label + "*")
}

// mrkdwnSection is a section block of mrkdwn text.
func mrkdwnSection(text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "section",
		"text": mrkdwnElement(text),
	}
}

// mrkdwnElement is a mrkdwn text object, as used in a section's text or fields and in a context block. It's
// verbatim: without that, Slack turns a plain @here, @channel or @everyone in a value into a real mention, which
// escaping can't stop. Links and dates written as <...> still work.
func mrkdwnElement(text string) map[string]interface{} {
	return map[string]interface{}{
		"type":     "mrkdwn",
		"text":     text,
		"verbatim": true,
	}
}
