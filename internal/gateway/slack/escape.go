package slack

import (
	"net/url"
	"strings"
	"unicode"
)

// mrkdwnEscaper escapes the characters Slack's mrkdwn reserves for links, mentions and formatting.
var mrkdwnEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// codeEscaper escapes a value shown as code. It also swaps backticks for a look-alike (ˋ), so the value can't close
// the code early and have the rest of it read as mrkdwn.
var codeEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "`", "ˋ")

// escapeMrkdwn escapes a value for mrkdwn text. Every value from CI, a user or an API token goes through it (or
// through inlineCode or codeBlock), so it shows as written and can't add a mention such as <!channel> or a link
// whose label hides where it goes.
func escapeMrkdwn(value string) string {
	return mrkdwnEscaper.Replace(value)
}

// inlineCode shows a value as inline code.
func inlineCode(value string) string {
	return "`" + codeEscaper.Replace(value) + "`"
}

// codeBlock shows a value as a code block.
func codeBlock(value string) string {
	return "```" + codeEscaper.Replace(value) + "```"
}

// mrkdwnLink links label, which must already be mrkdwn, to target.
func mrkdwnLink(target, label string) string {
	return "<" + escapeMrkdwn(target) + "|" + label + ">"
}

// externalLink links label to a URL the gateway didn't build itself (a CI build URL, or a pull request URL from
// GitHub), or returns "" when the URL isn't safe to link and the caller should leave the link out. Only https URLs
// with a host are linked, without user info ("https://github.com@evil.example" reads as GitHub but isn't) and
// without anything that would end the link early or change its label.
func externalLink(rawURL, label string) string {
	if strings.ContainsAny(rawURL, "|<>") || strings.ContainsFunc(rawURL, unicode.IsSpace) {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return mrkdwnLink(rawURL, label)
}
