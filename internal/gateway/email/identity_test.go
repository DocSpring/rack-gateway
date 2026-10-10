package email

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordedEmail struct {
	to                     []string
	subject, text, htmlStr string
}

type recordingSender struct{ sent []recordedEmail }

func (r *recordingSender) Send(to, subject, textBody, htmlBody string) error {
	r.sent = append(r.sent, recordedEmail{to: []string{to}, subject: subject, text: textBody, htmlStr: htmlBody})
	return nil
}

func (r *recordingSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	r.sent = append(r.sent, recordedEmail{to: to, subject: subject, text: textBody, htmlStr: htmlBody})
	return nil
}

func TestIdentifiedSenderNamesTheGatewayInEveryEmail(t *testing.T) {
	inner := &recordingSender{}
	sender := NewIdentifiedSender(inner, Identity{Name: "Staging", URL: "https://rack-gateway-staging.example.ts.net/"})

	require.NoError(t, sender.Send("a@example.com", "New CLI Login", "Hello,\n\nDetails.\n",
		"<!DOCTYPE html><html><body><p>Hello</p></body></html>"))
	require.NoError(t, sender.SendMany([]string{"a@example.com", "b@example.com"}, "Account Locked", "Locked.", ""))

	require.Len(t, inner.sent, 2)
	single := inner.sent[0]
	require.Equal(t, "Rack Gateway (Staging): New CLI Login", single.subject)
	require.Equal(t,
		"Hello,\n\nDetails.\n\n--\nSent by the Staging Rack Gateway (https://rack-gateway-staging.example.ts.net).\n",
		single.text)
	require.Contains(t, single.htmlStr, "<p>Hello</p><hr")
	require.Contains(t, single.htmlStr,
		`Sent by the <strong>Staging Rack Gateway</strong> (<a href="https://rack-gateway-staging.example.ts.net"`)
	require.True(t, strings.HasSuffix(single.htmlStr, "</p></body></html>"), "footer goes inside <body>")

	many := inner.sent[1]
	require.Equal(t, []string{"a@example.com", "b@example.com"}, many.to)
	require.Equal(t, "Rack Gateway (Staging): Account Locked", many.subject)
	require.Contains(t, many.text, "Sent by the Staging Rack Gateway")
	require.Empty(t, many.htmlStr, "a text-only email stays text-only")
}

func TestIdentifiedSenderAppendsFooterToHTMLFragments(t *testing.T) {
	inner := &recordingSender{}
	sender := NewIdentifiedSender(inner, Identity{Name: "EU", URL: "https://gw.example.com"})

	require.NoError(t, sender.Send("a@example.com", "Subject", "Text", "<p>Fragment</p>"))
	require.Regexp(t, `^<p>Fragment</p><hr .*Sent by the <strong>EU Rack Gateway</strong>.*</p>$`,
		inner.sent[0].htmlStr)
}

func TestIdentifiedSenderEscapesIdentityInHTML(t *testing.T) {
	inner := &recordingSender{}
	sender := NewIdentifiedSender(inner, Identity{Name: `<b>"x"</b>`, URL: `https://gw.example.com/?a=1&b="2"`})

	require.NoError(t, sender.Send("a@example.com", "Subject", "Text", "<p>Body</p>"))
	html := inner.sent[0].htmlStr
	require.NotContains(t, html, `<b>"x"</b>`)
	require.Contains(t, html, "&lt;b&gt;&#34;x&#34;&lt;/b&gt; Rack Gateway")
	require.Contains(t, html, `href="https://gw.example.com/?a=1&amp;b=&#34;2&#34;"`)
}

func TestIdentifiedSenderWithoutIdentity(t *testing.T) {
	inner := &recordingSender{}
	sender := NewIdentifiedSender(inner, Identity{})

	require.NoError(t, sender.Send("a@example.com", "Subject", "Text", ""))
	require.Equal(t, "Rack Gateway: Subject", inner.sent[0].subject)
	require.Equal(t, "Text\n\n--\nSent by the Rack Gateway.\n", inner.sent[0].text)
}
