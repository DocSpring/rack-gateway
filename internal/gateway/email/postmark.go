package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// Sender is a minimal interface for sending emails.
type Sender interface {
	Send(to, subject, textBody, htmlBody string) error
	SendMany(to []string, subject, textBody, htmlBody string) error
}

// NoopSender implements Sender but does nothing (used when not configured).
type NoopSender struct{}

// Send implements Sender.Send but does nothing.
func (NoopSender) Send(_, _, _, _ string) error { return nil }

// SendMany implements Sender.SendMany but does nothing.
func (NoopSender) SendMany(_ []string, _, _, _ string) error { return nil }

// PostmarkSender sends emails using Postmark's API.
type PostmarkSender struct {
	Token   string
	From    string
	Stream  string
	APIBase string
	client  *http.Client
}

var postmarkHTTPClient = &http.Client{Timeout: 10 * time.Second}

// NewSender chooses the best available sender based on token and dev flags.
// - With POSTMARK token -> PostmarkSender (at POSTMARK_API_BASE in a testbypass build)
// - Else if DEV_EMAIL_LOG=true or dev mode -> LoggerSender (prints to stdout)
// - Else -> NoopSender
func NewSender(token, from, stream string) Sender {
	if token != "" {
		if stream == "" {
			stream = "outbound"
		}
		apiBase := testbypass.PostmarkAPIBase()
		if apiBase == "" {
			apiBase = "https://api.postmarkapp.com"
		}
		return &PostmarkSender{
			Token:   token,
			From:    from,
			Stream:  stream,
			APIBase: apiBase,
			client:  postmarkHTTPClient,
		}
	}
	if os.Getenv("DEV_EMAIL_LOG") == "true" || testbypass.DevMode() {
		return &LoggerSender{From: from}
	}
	return NoopSender{}
}

// sendEmail builds the Postmark API request and sends it.
// The 'to' parameter is the primary recipient, and 'bcc' is an optional
// comma-separated list of additional recipients.
func (p *PostmarkSender) sendEmail(to, bcc, subject, textBody, htmlBody string) error {
	if to == "" {
		return nil
	}
	payload := map[string]string{
		"From":          p.From,
		"To":            to,
		"Subject":       subject,
		"TextBody":      textBody,
		"MessageStream": p.Stream,
	}
	if htmlBody != "" {
		payload["HtmlBody"] = htmlBody
	}
	if bcc != "" {
		payload["Bcc"] = bcc
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", p.APIBase+"/email", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.Token)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("postmark send failed: %s", resp.Status)
	}
	return nil
}

// Send sends an email to a single recipient via Postmark.
func (p *PostmarkSender) Send(to, subject, textBody, htmlBody string) error {
	return p.sendEmail(to, "", subject, textBody, htmlBody)
}

// SendMany sends an email to multiple recipients via Postmark using BCC.
func (p *PostmarkSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	if len(to) == 0 {
		return nil
	}
	// Use Bcc for additional recipients to avoid exposing multiple To addresses
	primary := to[0]
	bcc := ""
	if len(to) > 1 {
		// Comma-separated per Postmark API
		for i, addr := range to[1:] {
			if i == 0 {
				bcc = addr
			} else {
				bcc += "," + addr
			}
		}
	}
	return p.sendEmail(primary, bcc, subject, textBody, htmlBody)
}

// LoggerSender writes emails to stdout (useful in development)
type LoggerSender struct{ From string }

// Send logs an email to stdout instead of sending it.
func (_ *LoggerSender) Send(to, subject, textBody, htmlBody string) error {
	gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s subject=%q", to, subject)
	if htmlBody != "" {
		gtwlog.DebugTopicf(gtwlog.TopicEmailBody, "text=%s\n\nhtml=%s", textBody, htmlBody)
	}
	return nil
}

// SendMany logs an email to multiple recipients to stdout instead of sending it.
func (_ *LoggerSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	primary := ""
	if len(to) > 0 {
		primary = to[0]
	}
	bcc := []string{}
	if len(to) > 1 {
		bcc = append(bcc, to[1:]...)
	}
	b, _ := json.Marshal(bcc)
	gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s bcc=%s subject=%q", primary, string(b), subject)
	if htmlBody != "" {
		gtwlog.DebugTopicf(gtwlog.TopicEmailBody, "text=%s html=%s", textBody, htmlBody)
	}
	return nil
}
