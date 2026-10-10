package app

import (
	"testing"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

func TestBuildSentryOptionsDisabledWithoutDSN(t *testing.T) {
	opts, enabled := buildSentryOptions(&config.Config{DevMode: true})
	if enabled {
		t.Fatalf("expected Sentry to be disabled when DSN is empty")
	}
	if opts.Dsn != "" {
		t.Fatalf("expected DSN to be empty, got %q", opts.Dsn)
	}
}

func TestBuildSentryOptionsDefaults(t *testing.T) {
	cfg := &config.Config{
		SentryDSN:         "https://examplePublicKey@o0.ingest.sentry.io/0",
		SentryEnvironment: "",
		SentryRelease:     "1.2.3",
		Domain:            "gateway.example.com",
		DevMode:           false,
	}
	opts, enabled := buildSentryOptions(cfg)
	if !enabled {
		t.Fatalf("expected Sentry to be enabled when DSN is provided")
	}
	if opts.Environment != "production" {
		t.Fatalf("expected default environment to be production, got %q", opts.Environment)
	}
	if opts.Release != "1.2.3" {
		t.Fatalf("expected release to be propagated, got %q", opts.Release)
	}
	if opts.ServerName != "gateway.example.com" {
		t.Fatalf("expected server name to match domain, got %q", opts.ServerName)
	}
}

func TestBuildSentryOptionsDevelopmentEnvironment(t *testing.T) {
	cfg := &config.Config{
		SentryDSN:         "https://examplePublicKey@o0.ingest.sentry.io/0",
		DevMode:           true,
		SentryEnvironment: "",
	}
	opts, enabled := buildSentryOptions(cfg)
	if !enabled {
		t.Fatalf("expected Sentry to be enabled when DSN is provided")
	}
	if opts.Environment != "development" {
		t.Fatalf("expected environment to default to development in dev mode, got %q", opts.Environment)
	}
}

func TestScrubSentryEventRemovesCredentials(t *testing.T) {
	opts, enabled := buildSentryOptions(&config.Config{SentryDSN: "https://key@o0.ingest.sentry.io/1"})
	if !enabled || opts.SendDefaultPII || opts.BeforeSend == nil {
		t.Fatalf("expected PII disabled and a scrubber, got SendDefaultPII=%v BeforeSend=%v",
			opts.SendDefaultPII, opts.BeforeSend != nil)
	}

	event := &sentry.Event{Request: &sentry.Request{
		Headers: map[string]string{
			"Authorization": "Basic Y29udm94OnNlc3Npb24udG90cC4xMjM0NTY=",
			"Cookie":        "session_token=abc",
			"X-CSRF-Token":  "csrf",
			"X-MFA-TOTP":    "123456",
			"User-Agent":    "rack-gateway/1.0",
		},
		Cookies:     "session_token=abc",
		QueryString: "state=secret&code=oauth",
		Data:        `{"set":{"SECRET":"x"}}`,
		Env:         map[string]string{"REMOTE_ADDR": "10.0.0.1"},
	}}
	scrubbed := opts.BeforeSend(event, nil)

	if got := scrubbed.Request.Headers; len(got) != 1 || got["User-Agent"] != "rack-gateway/1.0" {
		t.Fatalf("expected only User-Agent to survive, got %v", got)
	}
	if scrubbed.Request.Cookies != "" || scrubbed.Request.QueryString != "" || scrubbed.Request.Data != "" ||
		scrubbed.Request.Env != nil {
		t.Fatalf("expected cookies, query string, body and env to be cleared: %+v", scrubbed.Request)
	}
}
