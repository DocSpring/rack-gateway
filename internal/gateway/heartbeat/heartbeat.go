// Package heartbeat pings heartbeat monitors (Cronitor, Healthchecks.io and the like): URLs the gateway requests
// on a schedule, so the monitor alerts when the requests stop. A gateway usually runs on a private network that an
// uptime check can't reach.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// pingTimeout bounds a ping.
const pingTimeout = 10 * time.Second

// Pinger requests a heartbeat URL. The URL usually carries the monitoring service's key, so it never appears in
// errors: they name the env var that set it instead.
type Pinger struct {
	name   string
	url    string
	client *http.Client
}

// FromEnv returns a Pinger for the https URL in the env var name, or nil when it isn't set.
func FromEnv(name string) (*Pinger, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil, nil
	}
	return newPinger(name, value, &http.Client{Timeout: pingTimeout})
}

func newPinger(name, rawURL string, client *http.Client) (*Pinger, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid %s: expected an https URL", name)
	}
	return &Pinger{name: name, url: parsed.String(), client: client}, nil
}

// Name is the env var that set the URL.
func (p *Pinger) Name() string {
	return p.name
}

// Ping requests the URL. It fails unless the monitor answers with a 2xx status.
func (p *Pinger) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("%s ping failed: invalid request", p.name)
	}
	req.Header.Set("User-Agent", "rack-gateway")
	resp, err := p.client.Do(req)
	if err != nil {
		// A *url.Error quotes the URL; the error it wraps doesn't.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("%s ping failed: %w", p.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s ping failed: HTTP %d", p.name, resp.StatusCode)
	}
	return nil
}
