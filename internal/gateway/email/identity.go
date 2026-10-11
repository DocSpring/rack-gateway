package email

import (
	"fmt"
	"html"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// IdentifiedSender wraps a Sender so every email names its gateway: the subject is prefixed with
// "Rack Gateway (<name>): " and a footer with the gateway's name and URL is appended to both bodies.
// Callers write subjects without a rack prefix.
type IdentifiedSender struct {
	inner    Sender
	identity config.GatewayIdentity
}

// NewIdentifiedSender returns a Sender that identifies the gateway in every email it sends through inner.
func NewIdentifiedSender(inner Sender, identity config.GatewayIdentity) *IdentifiedSender {
	return &IdentifiedSender{inner: inner, identity: identity}
}

// Send sends an identified email to a single recipient.
func (s *IdentifiedSender) Send(to, subject, textBody, htmlBody string) error {
	return s.inner.Send(to, s.identifySubject(subject), s.identifyText(textBody), s.identifyHTML(htmlBody))
}

// SendMany sends an identified email to several recipients.
func (s *IdentifiedSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	return s.inner.SendMany(to, s.identifySubject(subject), s.identifyText(textBody), s.identifyHTML(htmlBody))
}

// identifySubject prefixes subject with the gateway's name.
func (s *IdentifiedSender) identifySubject(subject string) string {
	return s.identity.Label() + ": " + subject
}

// identifyText appends the gateway footer to a plain-text body.
func (s *IdentifiedSender) identifyText(body string) string {
	return strings.TrimRight(body, "\n") + "\n\n--\n" + s.footerText() + "\n"
}

// identifyHTML appends the gateway footer to an HTML body (inside <body> when there is one). An empty body
// stays empty, so text-only emails remain text-only.
func (s *IdentifiedSender) identifyHTML(body string) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	footer := `<hr style="border:none;border-top:1px solid #e5e5e5;margin:24px 0;"/>` +
		`<p style="font-size:12px;color:#555;">` + s.footerHTML() + `</p>`
	if i := lastBodyCloseTag(body); i >= 0 {
		return body[:i] + footer + body[i:]
	}
	return body + footer
}

const bodyCloseTag = "</body>"

// lastBodyCloseTag returns the byte index of the last </body> in s, ignoring ASCII case, or -1.
// It compares bytes in place: lowercasing s first would shift the index whenever s contains non-ASCII letters
// whose lowercase form has a different UTF-8 length (e.g. 'Ⱥ', the Kelvin sign).
func lastBodyCloseTag(s string) int {
	for i := len(s) - len(bodyCloseTag); i >= 0; i-- {
		if equalASCIIFold(s[i:i+len(bodyCloseTag)], bodyCloseTag) {
			return i
		}
	}
	return -1
}

func equalASCIIFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func (s *IdentifiedSender) footerText() string {
	if s.identity.URL == "" {
		return "Sent by the " + s.identity.FullName() + "."
	}
	return fmt.Sprintf("Sent by the %s (%s).", s.identity.FullName(), s.identity.URL)
}

func (s *IdentifiedSender) footerHTML() string {
	name := "<strong>" + html.EscapeString(s.identity.FullName()) + "</strong>"
	if s.identity.URL == "" {
		return "Sent by the " + name + "."
	}
	url := html.EscapeString(s.identity.URL)
	return fmt.Sprintf(`Sent by the %s (<a href="%s" style="color:#0b5fff;">%s</a>).`, name, url, url)
}
