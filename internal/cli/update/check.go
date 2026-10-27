package update

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"
)

// CheckInterval is how long the CLI waits after checking GitHub before it checks again.
const CheckInterval = 24 * time.Hour

// checkState is what the last update check found. The CLI saves it in its config directory.
type checkState struct {
	CheckedAt time.Time `json:"checked_at"`
	LatestTag string    `json:"latest_tag,omitempty"`
}

// Checker checks GitHub for a newer release at most once per CheckInterval.
type Checker struct {
	Client  *Client
	Current string
	// Load returns the saved state, or an error when there is none. Save replaces it.
	Load func() ([]byte, error)
	Save func([]byte) error
	Now  func() time.Time
}

// Check is an update check that may still be running in the background.
type Check struct {
	client  *Client
	current string
	saved   checkState
	done    chan struct{}
	cancel  context.CancelFunc
	fresh   *checkState
}

// Start begins a background check when the last one is more than CheckInterval old. It never blocks.
func (c *Checker) Start() *Check {
	check := &Check{client: c.Client, current: c.Current}
	if data, err := c.Load(); err == nil {
		_ = json.Unmarshal(data, &check.saved)
	}
	if c.Now().Sub(check.saved.CheckedAt) < CheckInterval {
		return check
	}
	ctx, cancel := context.WithCancel(context.Background())
	check.done = make(chan struct{})
	check.cancel = cancel
	go func() {
		defer close(check.done)
		check.fresh = c.fetch(ctx, check.saved)
	}()
	return check
}

// fetch asks GitHub for the latest release and saves what it found. A failed check is saved too, so an offline
// machine doesn't try on every command, but one canceled because the command finished first is not.
func (c *Checker) fetch(ctx context.Context, saved checkState) *checkState {
	state := checkState{CheckedAt: c.Now(), LatestTag: saved.LatestTag}
	rel, err := c.Client.LatestRelease(ctx)
	if err != nil && ctx.Err() != nil {
		return nil
	}
	if err == nil {
		state.LatestTag = rel.Tag
	}
	if data, err := json.Marshal(state); err == nil {
		_ = c.Save(data)
	}
	return &state
}

// Notice returns the message to show when a newer release is available, or "". It waits at most wait for a check
// still running, then cancels it and uses the last saved result.
func (k *Check) Notice(wait time.Duration) string {
	state := k.saved
	if k.done != nil {
		timer := time.NewTimer(wait)
		select {
		case <-k.done:
			if k.fresh != nil {
				state = *k.fresh
			}
		case <-timer.C:
		}
		timer.Stop()
		k.cancel()
	}
	if state.LatestTag == "" || !IsNewer(state.LatestTag, k.current) {
		return ""
	}
	latest := Release{Tag: state.LatestTag}
	if _, ok := AssetName(runtime.GOOS, runtime.GOARCH); !ok {
		return fmt.Sprintf("A new release of rack-gateway is available: %s → %s. Build it from source: %s",
			k.current, latest.Version(), k.client.ReleaseURL(latest.Tag))
	}
	return fmt.Sprintf(
		"A new release of rack-gateway is available: %s → %s. Run \"rack-gateway update\" to install it.",
		k.current, latest.Version())
}
