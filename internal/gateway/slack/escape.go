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

// codeBlock shows a value as a code block, shortened to fit in a section.
func codeBlock(value string) string {
	return "```" + truncateMrkdwn(codeEscaper.Replace(value), maxSectionValue) + "```"
}

// maxSectionValue is how long a value in a section's text may be. Slack refuses a whole message whose section text
// is over 3000 characters, so a long value (or one that escaping made longer) must leave room for its label.
const maxSectionValue = 2500

// truncateMrkdwn shortens escaped mrkdwn to at most limit characters, ending with "…". It doesn't cut an escape
// such as &amp; in two.
func truncateMrkdwn(escaped string, limit int) string {
	runes := []rune(escaped)
	if len(runes) <= limit {
		return escaped
	}
	cut := string(runes[:limit-1])
	if amp := strings.LastIndexByte(cut, '&'); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut + "…"
}

// mrkdwnLink links label, which must already be mrkdwn, to target.
func mrkdwnLink(target, label string) string {
	return "<" + escapeMrkdwn(target) + "|" + label + ">"
}

// externalLink links label to a URL the gateway didn't build itself, such as a pull request URL from GitHub, or
// returns "" when the URL isn't safe to link and the caller should leave the link out (see safeExternalURL).
func externalLink(rawURL, label string) string {
	if safeExternalURL(rawURL) == nil {
		return ""
	}
	return mrkdwnLink(rawURL, label)
}

// hostLabeledLink is externalLink for a URL whose host the gateway can't check, such as a CI build URL from the
// request: the label names the host, so the link can't pass for something it isn't.
func hostLabeledLink(rawURL, label string) string {
	parsed := safeExternalURL(rawURL)
	if parsed == nil {
		return ""
	}
	return mrkdwnLink(rawURL, label+" ("+escapeMrkdwn(parsed.Hostname())+")")
}

// safeExternalURL parses a URL that's safe to link, or returns nil. Only https URLs with an ASCII host are safe
// (a non-ASCII host can imitate a familiar one), without user info ("https://github.com@evil.example" reads as
// GitHub but isn't) and without anything that would end the link early or change its label.
func safeExternalURL(rawURL string) *url.URL {
	if strings.ContainsAny(rawURL, "|<>") || strings.ContainsFunc(rawURL, unicode.IsSpace) {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return nil
	}
	host := parsed.Hostname()
	if host == "" || strings.ContainsFunc(host, func(r rune) bool { return r > unicode.MaxASCII }) {
		return nil
	}
	return parsed
}
