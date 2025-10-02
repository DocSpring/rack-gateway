package app

import (
	"testing"

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
