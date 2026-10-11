package slack

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// hostile is what someone holding a CI token, or naming a user or token, might put in a value: a channel mention,
// a link labeled as something it isn't, a bare ampersand, and a code fence.
const hostile = "a & b <!channel> <https://evil.example/login|GitHub PR> ```"

// hostileText is hostile escaped for mrkdwn text.
const hostileText = "a &amp; b &lt;!channel&gt; &lt;https://evil.example/login|GitHub PR&gt; ```"

// hostileCode is hostile escaped for a code span or block: its backticks can't close the code.
const hostileCode = "a &amp; b &lt;!channel&gt; &lt;https://evil.example/login|GitHub PR&gt; ˋˋˋ"

// mrkdwnTexts returns a message's fallback text and the text of every mrkdwn object in its blocks, and checks that
// each object is verbatim, so Slack doesn't turn a plain @here in a value into a mention.
func mrkdwnTexts(t *testing.T, text string, blocks []map[string]interface{}) []string {
	t.Helper()
	objects := []map[string]interface{}{}
	for _, block := range blocks {
		if blockText, ok := block["text"].(map[string]interface{}); ok {
			objects = append(objects, blockText)
		}
		for _, key := range []string{"fields", "elements"} {
			elements, _ := block[key].([]map[string]interface{})
			objects = append(objects, elements...)
		}
	}
	texts := make([]string, 0, 1+len(objects))
	texts = append(texts, text)
	for _, object := range objects {
		require.Equal(t, true, object["verbatim"], object["text"])
		texts = append(texts, object["text"].(string))
	}
	return texts
}

// requireInert fails if any text still carries the hostile value's mention, disguised link or bare ampersand.
func requireInert(t *testing.T, texts []string) {
	t.Helper()
	for _, s := range texts {
		require.NotContains(t, s, "<!channel>")
		require.NotContains(t, s, "<https://evil.example")
		require.NotContains(t, s, "|GitHub PR>")
		require.NotContains(t, s, "a & b")
	}
}

func TestEscapeMrkdwnAndCode(t *testing.T) {
	require.Equal(t, hostileText, escapeMrkdwn(hostile))
	require.Equal(t, "`"+hostileCode+"`", inlineCode(hostile))
	require.Equal(t, "```"+hostileCode+"```", codeBlock(hostile))
	require.Equal(t, "```ˋˋˋ\n*bold* &lt;!here&gt;\nˋˋ```", codeBlock("```\n*bold* <!here>\n``"))
}

func TestExternalLinkOnlyLinksSafeHTTPSURLs(t *testing.T) {
	require.Equal(t, "<https://github.com/org/repo/pull/42|GitHub PR>",
		externalLink("https://github.com/org/repo/pull/42", "GitHub PR"))
	require.Equal(t, "<https://ci.example.com/build?id=1&amp;step=2|CI Pipeline>",
		externalLink("https://ci.example.com/build?id=1&step=2", "CI Pipeline"))

	refused := []string{
		"",
		"http://ci.example.com/build/1",
		"https://evil.example/login|GitHub PR",
		"https://ci.example.com/1>|<!channel>",
		"https://ci.example.com/<!channel>",
		"https://ci.example.com/build 1",
		"https://ci.example.com/build\n1",
		"https://github.com@evil.example/org/repo/pull/42",
		"https:evil.example",
		"//evil.example/build/1",
		"javascript:alert(1)",
		"slack://open",
		"ci.example.com/build/1",
		"https://:443/build/1",
		"https://g\u0456thub.com/org/repo/pull/42", // a Cyrillic і
	}
	for _, rawURL := range refused {
		require.Empty(t, externalLink(rawURL, "CI Pipeline"), rawURL)
		require.Empty(t, hostLabeledLink(rawURL, "CI Pipeline"), rawURL)
	}
}

func TestHostLabeledLinkNamesTheHost(t *testing.T) {
	require.Equal(t, "<https://ci.example.com:8443/build/1|CI Pipeline (ci.example.com)>",
		hostLabeledLink("https://ci.example.com:8443/build/1", "CI Pipeline"))
}

// Slack refuses a whole message whose section text is too long, so long values are cut short, without cutting an
// escape in two.
func TestLongValuesAreTruncated(t *testing.T) {
	message := strings.Repeat("&", 3000)
	section := sectionText(t, deployApprovalMessageBlock(&db.DeployApprovalRequest{Message: message}))
	value := strings.TrimPrefix(section, "*Message:*\n")
	require.LessOrEqual(t, len([]rune(value)), maxSectionValue)
	require.True(t, strings.HasSuffix(value, "&amp;…"), value[len(value)-20:])

	details := codeBlock(strings.Repeat("<", 3000))
	require.LessOrEqual(t, len([]rune(details)), maxSectionValue+6)
	require.True(t, strings.HasSuffix(details, "&lt;…```"))
	require.Equal(t, 2, strings.Count(details, "```"))

	require.Equal(t, "short", truncateMrkdwn("short", 10))
}

func TestDeployApprovalAlertEscapesRequestValues(t *testing.T) {
	notifier := NewNotifier(nil, testGateway)
	ciMetadata, err := json.Marshal(map[string]interface{}{"build_url": "http://ci.example.com/build/1"})
	require.NoError(t, err)
	req := &db.DeployApprovalRequest{
		PublicID:       "req_abc123",
		App:            hostile,
		GitBranch:      hostile,
		GitCommitHash:  "abc123def456789",
		Message:        hostile,
		PrURL:          "https://evil.example/login|GitHub PR",
		CIMetadata:     ciMetadata,
		CreatedByName:  hostile,
		CreatedByEmail: hostile,
		CreatedAt:      time.Now(),
	}

	text, blocks := notifier.formatDeployApprovalAlert(req)

	require.Equal(t, "Rack Gateway (Staging): 🚀 Deploy approval needed for "+hostileText+" - "+hostileText, text)
	require.Equal(t, "*🚀 Deploy approval needed for `"+hostileCode+"`*", sectionText(t, blocks[1]))
	details, ok := blocks[2]["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "*App:*\n"+hostileText, details[0]["text"])
	require.Equal(t, "*Message:*\n"+hostileText, sectionText(t, blocks[3]))

	// Neither the PR URL nor the CI build URL is safe to link, so only the gateway's own link is left.
	links, ok := blocks[4]["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, links, 1)
	require.Equal(t,
		"🔗 <https://gateway.example.com/app/deploy-approval-requests/req_abc123|View Approval Request>",
		links[0]["text"])

	creator, ok := blocks[5]["elements"].([]map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "Created by "+hostileText+" ("+hostileText+")", creator[0]["text"])

	requireInert(t, mrkdwnTexts(t, text, blocks))
}

func TestDeployApprovalCreatorIsEscaped(t *testing.T) {
	require.Equal(t, "Created by "+hostileText,
		deployApprovalCreatorText(&db.DeployApprovalRequest{CreatedByEmail: hostile}))
	require.Equal(t, "Created by API token: "+hostileText,
		deployApprovalCreatorText(&db.DeployApprovalRequest{CreatedByAPITokenName: hostile}))
}

func TestDeployApprovalAlertLinksSafePRAndCIURLs(t *testing.T) {
	ciMetadata, err := json.Marshal(map[string]interface{}{"build_url": "https://ci.example.com/build/1?step=2&x=3"})
	require.NoError(t, err)
	req := &db.DeployApprovalRequest{
		PublicID:   "req_abc123",
		PrURL:      "https://github.com/org/repo/pull/42",
		CIMetadata: ciMetadata,
	}

	links, ok := deployApprovalLinksBlock(req, "https://gateway.example.com")["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, links, 3)
	require.Equal(t, "🔗 <https://github.com/org/repo/pull/42|GitHub PR>", links[1]["text"])
	require.Equal(t, "🔗 <https://ci.example.com/build/1?step=2&amp;x=3|CI Pipeline (ci.example.com)>", links[2]["text"])
}

func TestAuditLogMessageEscapesLogValues(t *testing.T) {
	notifier := NewNotifier(nil, testGateway)
	auditLog := &db.AuditLog{
		Action:       hostile,
		UserName:     hostile,
		UserEmail:    hostile,
		Resource:     hostile,
		ResourceType: hostile,
		Status:       hostile,
		IPAddress:    hostile,
		Details:      hostile + "\n" + hostile,
		Timestamp:    time.Now(),
	}

	text, blocks := notifier.formatAuditLogMessage(auditLog)

	user := hostileText + " (" + hostileText + ")"
	require.Equal(t, "Rack Gateway (Staging): 📝 *"+hostileText+"* - "+user, text)
	require.Equal(t, "*📝 "+hostileText+"*", sectionText(t, blocks[1]))

	fields, ok := blocks[2]["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "*User:*\n"+user, fields[0]["text"])
	require.Equal(t, "*Resource:*\n"+hostileText+" ("+hostileText+")", fields[1]["text"])
	require.Equal(t, "*Status:*\n"+hostileText, fields[2]["text"])

	// The details stay inside one code block: the only fences are the block's own.
	details := sectionText(t, blocks[3])
	require.Equal(t, "*Details:*\n```"+hostileCode+"\n"+hostileCode+"```", details)
	require.Equal(t, 2, strings.Count(details, "```"))

	ipContext, ok := blocks[4]["elements"].([]map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "IP: "+hostileText, ipContext[0]["text"])

	requireInert(t, mrkdwnTexts(t, text, blocks))
}

func TestAuditLogAPITokenNameIsEscaped(t *testing.T) {
	require.Equal(t, "API Token: "+hostileText, formatUserIdentifier(&db.AuditLog{APITokenName: hostile}))
	require.Equal(t, hostileText, formatUserIdentifier(&db.AuditLog{UserEmail: hostile}))
}
