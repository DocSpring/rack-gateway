package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
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
// - With POSTMARK token -> PostmarkSender
// - Else if DEV_EMAIL_LOG=true or DEV_MODE=true -> LoggerSender (prints to stdout)
// - Else -> NoopSender
func NewSender(token, from, stream string) Sender {
	if token != "" {
		if stream == "" {
			stream = "outbound"
		}
		return &PostmarkSender{
			Token:   token,
			From:    from,
			Stream:  stream,
			APIBase: getEnv("POSTMARK_API_BASE", "https://api.postmarkapp.com"),
			client:  postmarkHTTPClient,
		}
	}
	if getEnv("DEV_EMAIL_LOG", "") == "true" || getEnv("DEV_MODE", "") == "true" {
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
	defer resp.Body.Close() //nolint:errcheck // ignore close error
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
func (l *LoggerSender) Send(to, subject, textBody, htmlBody string) error {
	if htmlBody != "" {
		gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s subject=%q", to, subject)
		gtwlog.DebugTopicf(gtwlog.TopicEmailBody, "text=%s\n\nhtml=%s", textBody, htmlBody)
		appendDevEmail([]string{to}, subject, textBody, htmlBody)
		return nil
	}
	gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s subject=%q", to, subject)
	appendDevEmail([]string{to}, subject, textBody, htmlBody)
	return nil
}

// SendMany logs an email to multiple recipients to stdout instead of sending it.
func (l *LoggerSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	primary := ""
	if len(to) > 0 {
		primary = to[0]
	}
	bcc := []string{}
	if len(to) > 1 {
		bcc = append(bcc, to[1:]...)
	}
	b, _ := json.Marshal(bcc)
	if htmlBody != "" {
		gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s bcc=%s subject=%q", primary, string(b), subject)
		gtwlog.DebugTopicf(gtwlog.TopicEmailBody, "text=%s html=%s", textBody, htmlBody)
		appendDevEmail(to, subject, textBody, htmlBody)
		return nil
	}
	gtwlog.DebugTopicf(gtwlog.TopicEmailSummary, "to=%s bcc=%s subject=%q", primary, string(b), subject)
	appendDevEmail(to, subject, textBody, htmlBody)
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// DevEmail represents an in-memory email for E2E tests and local development.
type DevEmail struct {
	To      []string  `json:"to"`
	Subject string    `json:"subject"`
	Text    string    `json:"text"`
	Html    string    `json:"html"`
	TS      time.Time `json:"ts"`
}

var (
	devMu     sync.Mutex
	devOutbox []DevEmail
	devMax    = 100
)

func appendDevEmail(to []string, subject, text, html string) {
	devMu.Lock()
	defer devMu.Unlock()
	devOutbox = append(devOutbox, DevEmail{To: to, Subject: subject, Text: text, Html: html, TS: time.Now().UTC()})
	if len(devOutbox) > devMax {
		devOutbox = devOutbox[len(devOutbox)-devMax:]
	}
}

// GetDevOutbox returns up to 'limit' most recent dev emails (newest last)
func GetDevOutbox(limit int) []DevEmail {
	devMu.Lock()
	defer devMu.Unlock()
	n := len(devOutbox)
	if limit <= 0 || limit > n {
		limit = n
	}
	start := n - limit
	out := make([]DevEmail, limit)
	copy(out, devOutbox[start:])
	return out
}
