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

func TestWorkFailsWhenThePingFails(t *testing.T) {
	worker := NewWorker(func(context.Context) error { return errors.New("UPTIME_HEARTBEAT_URL ping failed: HTTP 500") })
	worker.health = func(time.Time) error { return nil }
	require.EqualError(t, worker.Work(context.Background(), nil), "UPTIME_HEARTBEAT_URL ping failed: HTTP 500")
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
