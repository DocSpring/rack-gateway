package uptime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWorker(health error) (*Worker, *int) {
	pings := 0
	worker := NewWorker(func(context.Context) error {
		pings++
		return nil
	})
	worker.health = func(time.Time) error { return health }
	return worker, &pings
}

func TestWorkPingsWhileHealthy(t *testing.T) {
	worker, pings := newTestWorker(nil)
	require.NoError(t, worker.Work(context.Background(), nil))
	assert.Equal(t, 1, *pings)
}

// A gateway that can't write its audit log refuses every request, so it doesn't report itself up.
func TestWorkDoesNotPingWhileUnhealthy(t *testing.T) {
	worker, pings := newTestWorker(errors.New("audit log unavailable"))
	require.NoError(t, worker.Work(context.Background(), nil))
	assert.Equal(t, 0, *pings)
}

// pingResults returns a worker whose pings fail or pass in the given order.
func pingResults(results ...error) *Worker {
	worker := NewWorker(func(context.Context) error {
		result := results[0]
		results = results[1:]
		return result
	})
	worker.health = func(time.Time) error { return nil }
	return worker
}

// One failed ping is usually the monitoring service or the network, and the heartbeat monitor alerts if pings stop:
// the job fails, and so reports, from the third failure in a row.
func TestWorkFailsWhenPingsKeepFailing(t *testing.T) {
	failed := errors.New("UPTIME_HEARTBEAT_URL ping failed: HTTP 500")
	worker := pingResults(failed, failed, failed, failed)
	require.NoError(t, worker.Work(context.Background(), nil))
	require.NoError(t, worker.Work(context.Background(), nil))
	require.EqualError(t, worker.Work(context.Background(), nil),
		"UPTIME_HEARTBEAT_URL ping failed: HTTP 500 (3 pings in a row have failed)")
	err := worker.Work(context.Background(), nil)
	require.ErrorIs(t, err, failed)
	assert.EqualError(t, err, "UPTIME_HEARTBEAT_URL ping failed: HTTP 500 (4 pings in a row have failed)")
}

func TestWorkCountsFailuresAgainAfterAPing(t *testing.T) {
	failed := errors.New("UPTIME_HEARTBEAT_URL ping failed: timeout")
	worker := pingResults(failed, failed, nil, failed, failed, failed)
	for range 5 {
		require.NoError(t, worker.Work(context.Background(), nil))
	}
	require.Error(t, worker.Work(context.Background(), nil))
}

func TestIntervalFromEnv(t *testing.T) {
	t.Setenv(IntervalEnv, "")
	interval, err := IntervalFromEnv()
	require.NoError(t, err)
	assert.Equal(t, DefaultInterval, interval)

	t.Setenv(IntervalEnv, "5")
	interval, err = IntervalFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, interval)

	for _, invalid := range []string{"0", "-1", "15m", "soon"} {
		t.Setenv(IntervalEnv, invalid)
		_, err = IntervalFromEnv()
		require.Error(t, err, invalid)
		assert.Contains(t, err.Error(), "invalid "+IntervalEnv)
	}
}
