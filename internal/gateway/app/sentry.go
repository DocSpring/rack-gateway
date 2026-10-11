package app

import (
	"fmt"
	"log"
	"strings"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// buildSentryOptions derives the Sentry client options from configuration.
// It returns the options alongside a boolean indicating whether Sentry should be enabled.
func buildSentryOptions(cfg *config.Config) (sentry.ClientOptions, bool) {
	var opts sentry.ClientOptions
	if cfg == nil {
		return opts, false
	}
	dsn := strings.TrimSpace(cfg.SentryDSN)
	if dsn == "" {
		return opts, false
	}
	opts.Dsn = dsn
	opts.AttachStacktrace = true
	// Requests carry session tokens, API tokens and inline MFA codes in headers, cookies and query strings,
	// and secrets in bodies (e.g. env vars). Never let the SDK attach them, and scrub anything that gets through.
	opts.DataCollection = noRequestDataCollection()
	opts.BeforeSend = scrubSentryEvent

	env := strings.TrimSpace(cfg.SentryEnvironment)
	if env == "" {
		if cfg.DevMode {
			env = "development"
		} else {
			env = "production"
		}
	}
	opts.Environment = env

	if release := strings.TrimSpace(cfg.SentryRelease); release != "" {
		opts.Release = release
	}

	if host := strings.TrimSpace(cfg.Domain); host != "" {
		opts.ServerName = host
	}

	return opts, true
}

// initializeSentry configures the global Sentry SDK when a DSN is present.
func initializeSentry(cfg *config.Config) (bool, error) {
	opts, enabled := buildSentryOptions(cfg)
	if !enabled {
		return false, nil
	}

	if err := sentry.Init(opts); err != nil {
		return false, fmt.Errorf("failed to initialize Sentry: %w", err)
	}

	log.Printf("Sentry enabled (environment=%s, release=%s)", opts.Environment, opts.Release)
	return true, nil
}

// sentryHeaderAllowlist lists the only request headers forwarded to Sentry.
var sentryHeaderAllowlist = map[string]bool{
	"accept":       true,
	"content-type": true,
	"user-agent":   true,
	"x-request-id": true,
}

// scrubSentryEvent removes credentials from request data before an event leaves the gateway.
func scrubSentryEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil || event.Request == nil {
		return event
	}
	req := event.Request
	for name := range req.Headers {
		if !sentryHeaderAllowlist[strings.ToLower(name)] {
			delete(req.Headers, name)
		}
	}
	req.Cookies = ""
	req.QueryString = ""
	req.Data = ""
	req.Env = nil
	return event
}

// noRequestDataCollection stops the SDK collecting any request data or user details by itself.
func noRequestDataCollection() *sentry.DataCollection {
	off := func() *sentry.KeyValueCollectionBehavior {
		return &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff}
	}
	return &sentry.DataCollection{
		UserInfo:    sentry.Set(false),
		Cookies:     off(),
		HTTPHeaders: &sentry.HeaderCollectionConfig{Request: off(), Response: off()},
		HTTPBodies:  []sentry.BodyType{},
		QueryParams: off(),
	}
}
