package app

import (
	"fmt"
	"log"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/getsentry/sentry-go"
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
	opts.SendDefaultPII = true

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
