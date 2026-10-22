package proxy

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// An env change waits for one already in progress to the same app, then reads the env that change left. Reading
// it first would write back the old env and silently undo the other change (a protected one included).
func TestEnvChangesToAnAppAreSerialized(t *testing.T) {
	f := newProtectedEnvFixture(t)
	held, err := f.database.LockAppEnv(context.Background(), "app")
	require.NoError(t, err)
	defer held.Release()

	req, _ := f.request(t, f.deployer, "PORT=4000", "", "", stepUpAgeFresh)
	done := make(chan int, 1)
	go func() { done <- f.proxy(req).Code }()

	select {
	case code := <-done:
		t.Fatalf("the change finished (%d) while another change held the app's env lock", code)
	case <-time.After(500 * time.Millisecond):
	}
	require.Empty(t, f.rack.releases())

	// The change in progress lands, then releases the lock.
	f.rack.setEnv("DATABASE_URL=postgres://changed\nADMIN_PASSWORD=hunter2\nPORT=3000")
	held.Release()

	require.Equal(t, http.StatusOK, <-done)
	releases := f.rack.releases()
	require.Len(t, releases, 1)
	require.Contains(t, releases[0].env, "DATABASE_URL=postgres://changed")
	require.Contains(t, releases[0].env, "PORT=4000")
}

// The lock is released when the request ends, whether or not it created a release. A masked value for a key the
// app doesn't have is refused after the lock is taken.
func TestEnvChangeReleasesTheAppEnvLock(t *testing.T) {
	f := newProtectedEnvFixture(t)
	for env, status := range map[string]int{
		"PORT=4000":                          http.StatusOK,
		"PORT=4000\nNEW_KEY=" + maskedSecret: http.StatusBadRequest,
	} {
		req, _ := f.request(t, f.deployer, env, "", "", stepUpAgeFresh)
		require.Equal(t, status, f.proxy(req).Code, env)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		lock, err := f.database.LockAppEnv(ctx, "app")
		cancel()
		require.NoError(t, err, env)
		lock.Release()
	}
}
