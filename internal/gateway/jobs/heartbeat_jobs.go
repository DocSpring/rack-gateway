package jobs

import (
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/heartbeat"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs/uptime"
)

// uptimeHeartbeatJob registers the uptime heartbeat, which pings UPTIME_HEARTBEAT_URL when the gateway starts and
// every UPTIME_HEARTBEAT_INTERVAL_MINUTES while it's healthy. It returns nil when the URL isn't set.
func uptimeHeartbeatJob(workers *river.Workers) (*river.PeriodicJob, error) {
	pinger, err := heartbeat.FromEnv(uptime.URLEnv)
	if err != nil || pinger == nil {
		return nil, err
	}
	interval, err := uptime.IntervalFromEnv()
	if err != nil {
		return nil, err
	}
	river.AddWorker(workers, uptime.NewWorker(pinger.Ping))
	return uptime.PeriodicJob(interval), nil
}
