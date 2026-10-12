package slack

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Slack's limits: it refuses a whole message with any text over them.
const (
	slackMaxText      = 3000 // a section's text, or any other text object
	slackMaxFieldText = 2000 // a section field
)

// longValue is longer than any limit, and escaping makes it five times longer still.
var longValue = strings.Repeat("&<>`", 5000)

// longPath makes a URL too long to fit in a field.
var longPath = "/" + strings.Repeat("a", slackMaxFieldText)

func runeCount(t *testing.T, text interface{}) int {
	t.Helper()
	s, ok := text.(string)
	require.True(t, ok, "text is not a string: %v", text)
	return utf8.RuneCountInString(s)
}

// requireFitsSlackLimits fails if Slack would refuse the message: if any section text, field or context element is
// too long. It also keeps the fallback text to maxFallbackText.
func requireFitsSlackLimits(t *testing.T, text string, blocks []map[string]interface{}) {
	t.Helper()
	require.LessOrEqual(t, utf8.RuneCountInString(text), maxFallbackText)
	for _, block := range blocks {
		if blockText, ok := block["text"].(map[string]interface{}); ok {
			require.LessOrEqual(t, runeCount(t, blockText["text"]), slackMaxText)
		}
		fields, _ := block["fields"].([]map[string]interface{})
		for _, field := range fields {
			require.LessOrEqual(t, runeCount(t, field["text"]), slackMaxFieldText)
		}
		elements, _ := block["elements"].([]map[string]interface{})
		for _, element := range elements {
			require.LessOrEqual(t, runeCount(t, element["text"]), slackMaxText)
		}
	}
}

// The largest deploy approval alert CI can cause still fits, so approvers still get it: long values are cut short,
// and links too long for a field are left out.
func TestLargestDeployApprovalAlertFitsSlackLimits(t *testing.T) {
	ciMetadata, err := json.Marshal(map[string]interface{}{"build_url": "https://ci.example.com" + longPath})
	require.NoError(t, err)
	req := &db.DeployApprovalRequest{
		PublicID:       "req_abc123",
		App:            longValue,
		GitBranch:      longValue,
		GitCommitHash:  longValue,
		Message:        longValue,
		PrURL:          "https://github.com" + longPath,
		CIMetadata:     ciMetadata,
		CreatedByName:  longValue,
		CreatedByEmail: longValue,
		CreatedAt:      time.Now(),
	}

	text, blocks := NewNotifier(nil, testGateway).formatDeployApprovalAlert(req)

	requireFitsSlackLimits(t, text, blocks)
	require.True(t, strings.HasPrefix(text, "Rack Gateway (Staging): 🚀 Deploy approval needed for "), text[:80])
	links, ok := blocks[4]["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, links, 1)
	require.Equal(t,
		"🔗 <https://gateway.example.com/app/deploy-approval-requests/req_abc123|View Approval Request>",
		links[0]["text"])
}

// A CI build URL's host is part of its link's label, so a long host can make the link too long too.
func TestDeployApprovalAlertLeavesOutCILinkWithLongHost(t *testing.T) {
	host := strings.Repeat("a", slackMaxFieldText) + ".example.com"
	ciMetadata, err := json.Marshal(map[string]interface{}{"build_url": "https://" + host + "/build/1"})
	require.NoError(t, err)

	links, ok := deployApprovalLinksBlock(
		&db.DeployApprovalRequest{PublicID: "req_abc123", CIMetadata: ciMetadata},
		"https://gateway.example.com",
	)["fields"].([]map[string]interface{})

	require.True(t, ok)
	require.Len(t, links, 1)
	require.Contains(t, links[0]["text"], "View Approval Request")
}

func TestLargestAuditLogMessageFitsSlackLimits(t *testing.T) {
	auditLog := &db.AuditLog{
		Action:       longValue,
		UserName:     longValue,
		UserEmail:    longValue,
		Resource:     longValue,
		ResourceType: longValue,
		Status:       longValue,
		IPAddress:    longValue,
		Details:      longValue,
		Timestamp:    time.Now(),
	}

	text, blocks := NewNotifier(nil, testGateway).formatAuditLogMessage(auditLog)
	requireFitsSlackLimits(t, text, blocks)

	auditLog.APITokenName = longValue
	text, blocks = NewNotifier(nil, testGateway).formatAuditLogMessage(auditLog)
	requireFitsSlackLimits(t, text, blocks)
}

func TestAppendLinkFieldOnlyAddsLinksThatFit(t *testing.T) {
	fits := "<https://ci.example.com/" + strings.Repeat("a", slackMaxFieldText-30) + "|CI>"
	require.Equal(t, slackMaxFieldText, utf8.RuneCountInString("🔗 "+fits))

	fields := appendLinkField(nil, fits)
	require.Len(t, fields, 1)
	require.Equal(t, "🔗 "+fits, fields[0]["text"])

	require.Len(t, appendLinkField(fields, "<https://ci.example.com/a"+fits[24:]), 1)
	require.Len(t, appendLinkField(fields, ""), 1)
}

func TestMrkdwnFieldShortensTheValueToFit(t *testing.T) {
	field := mrkdwnField("App", escapeMrkdwn(strings.Repeat("&", slackMaxFieldText)))
	text, ok := field["text"].(string)
	require.True(t, ok)
	require.LessOrEqual(t, utf8.RuneCountInString(text), slackMaxFieldText)
	require.True(t, strings.HasPrefix(text, "*App:*\n&amp;"))
	require.True(t, strings.HasSuffix(text, "&amp;…"), text[len(text)-20:])
	require.Equal(t, true, field["verbatim"])

	require.Equal(t, "*App:*\nmyapp", mrkdwnField("App", "myapp")["text"])
}
