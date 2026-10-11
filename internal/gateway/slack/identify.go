package slack

import (
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// mrkdwnEscaper escapes the characters Slack's mrkdwn reserves for links, mentions and formatting.
var mrkdwnEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// IdentifyText starts a message's fallback text with the gateway's label, as email subjects do:
// "Rack Gateway (Staging): ...". The fallback text is what Slack shows in notifications.
func IdentifyText(gateway config.GatewayIdentity, text string) string {
	return mrkdwnEscaper.Replace(gateway.Label()) + ": " + text
}

// identify names the gateway in a message, since one Slack workspace can get messages from several gateways:
// the fallback text starts with the gateway's label, and the first block is that label, linked to the gateway.
func (n *Notifier) identify(text string, blocks []map[string]interface{}) (string, []map[string]interface{}) {
	return IdentifyText(n.gateway, text), append([]map[string]interface{}{n.gatewayBlock()}, blocks...)
}

// gatewayBlock is a section naming the gateway in bold, linked to the gateway when it has a URL.
func (n *Notifier) gatewayBlock() map[string]interface{} {
	label := mrkdwnEscaper.Replace(n.gateway.Label())
	if n.gateway.URL != "" {
		label = fmt.Sprintf("<%s|%s>", mrkdwnEscaper.Replace(n.gateway.URL), label)
	}
	return map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": "*" + label + "*",
		},
	}
}
