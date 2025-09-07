package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// Sender is a minimal interface for sending emails.
type Sender interface {
	Send(to, subject, textBody string) error
	SendMany(to []string, subject, textBody string) error
}

// NoopSender implements Sender but does nothing (used when not configured).
type NoopSender struct{}

func (NoopSender) Send(to, subject, textBody string) error              { return nil }
func (NoopSender) SendMany(to []string, subject, textBody string) error { return nil }

// PostmarkSender sends emails using Postmark's API.
type PostmarkSender struct {
	Token   string
	From    string
	Stream  string
	APIBase string
	client  *http.Client
}

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
			client:  &http.Client{},
		}
	}
	if getEnv("DEV_EMAIL_LOG", "") == "true" || getEnv("DEV_MODE", "") == "true" {
		return &LoggerSender{From: from}
	}
	return NoopSender{}
}

func (p *PostmarkSender) Send(to, subject, textBody string) error {
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("postmark send failed: %s", resp.Status)
	}
	return nil
}

func (p *PostmarkSender) SendMany(to []string, subject, textBody string) error {
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
	payload := map[string]string{
		"From":          p.From,
		"To":            primary,
		"Subject":       subject,
		"TextBody":      textBody,
		"MessageStream": p.Stream,
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("postmark send failed: %s", resp.Status)
	}
	return nil
}

// LoggerSender writes emails to stdout (useful in development)
type LoggerSender struct{ From string }

func (l *LoggerSender) Send(to, subject, textBody string) error {
	log.Printf("[DEV EMAIL] To=%s From=%s Subject=%q\n%s", to, l.From, subject, textBody)
	return nil
}

func (l *LoggerSender) SendMany(to []string, subject, textBody string) error {
	primary := ""
	if len(to) > 0 {
		primary = to[0]
	}
	bcc := []string{}
	if len(to) > 1 {
		bcc = append(bcc, to[1:]...)
	}
	b, _ := json.Marshal(bcc)
	log.Printf("[DEV EMAIL] To=%s BCC=%s From=%s Subject=%q\n%s", primary, string(b), l.From, subject, textBody)
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
