// Package uptime pings the uptime heartbeat (UPTIME_HEARTBEAT_URL) on a schedule while the gateway is healthy, so
// the heartbeat monitor alerts when the gateway is down, its background jobs have stopped (the ping is one) or it
// can't write its audit log.
package uptime

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Env vars that configure the uptime heartbeat.
const (
	// URLEnv is the heartbeat URL. Unset, the gateway doesn't ping.
	URLEnv = "UPTIME_HEARTBEAT_URL"
	// IntervalEnv is how often it pings, in minutes (DefaultInterval when unset).
	IntervalEnv = "UPTIME_HEARTBEAT_INTERVAL_MINUTES"
)

// DefaultInterval is how often the gateway pings unless UPTIME_HEARTBEAT_INTERVAL_MINUTES says otherwise.
const DefaultInterval = 15 * time.Minute

// IntervalFromEnv returns UPTIME_HEARTBEAT_INTERVAL_MINUTES, or DefaultInterval when it isn't set.
func IntervalFromEnv() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(IntervalEnv))
	if value == "" {
		return DefaultInterval, nil
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes <= 0 {
		return 0, fmt.Errorf("invalid %s: expected a whole number of minutes, 1 or more, got %q", IntervalEnv, value)
	}
	return time.Duration(minutes) * time.Minute, nil
}

// Args schedules a ping.
type Args struct{}

// Kind returns the unique identifier for this job type.
func (Args) Kind() string { return "heartbeat:uptime" }

// Worker pings the uptime heartbeat.
type Worker struct {
	river.WorkerDefaults[Args]
	ping func(context.Context) error
	// health is the health check's audit check (audit.WriteHealth).
	health func(time.Time) error
	now    func() time.Time
}

// NewWorker returns the worker; ping requests the heartbeat URL.
func NewWorker(ping func(context.Context) error) *Worker {
	return &Worker{ping: ping, health: audit.WriteHealth, now: time.Now}
}

// Work pings the heartbeat unless the health check would fail: a gateway that refuses every request because it
// can't write its audit log isn't up. A failed ping isn't retried; the next one is due within the interval.
func (w *Worker) Work(ctx context.Context, _ *river.Job[Args]) error {
	if err := w.health(w.now()); err != nil {
		gtwlog.Errorf("Uptime heartbeat: not pinging, the gateway is unhealthy: %v", err)
		return nil
	}
	return w.ping(ctx)
}

// PeriodicJob pings when the gateway starts and every interval.
func PeriodicJob(interval time.Duration) *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) { return Args{}, &river.InsertOpts{MaxAttempts: 1} },
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}
