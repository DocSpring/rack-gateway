package update

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

type memoryState struct {
	data  []byte
	saves atomic.Int32
}

func (m *memoryState) checker(client *Client, current string) *Checker {
	return &Checker{
		Client:  client,
		Current: BuildInfo{Version: current},
		GOOS:    "linux",
		GOARCH:  "amd64",
		Load: func() ([]byte, error) {
			if m.data == nil {
				return nil, errors.New("no state")
			}
			return m.data, nil
		},
		Save: func(data []byte) error {
			m.data = data
			m.saves.Add(1)
			return nil
		},
		Now: func() time.Time { return testNow },
	}
}

func (m *memoryState) set(t *testing.T, state checkState) {
	t.Helper()
	data, err := json.Marshal(state)
	require.NoError(t, err)
	m.data = data
}

func (m *memoryState) get(t *testing.T) checkState {
	t.Helper()
	var state checkState
	require.NoError(t, json.Unmarshal(m.data, &state))
	return state
}

func latestHandler(t *testing.T, requests *atomic.Int32, tag string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	})
}

// requireNotice checks notice suggests updating from 0.1.28 to 0.1.29.
func requireNotice(t *testing.T, notice string) {
	t.Helper()
	require.Equal(t, `A new release of rack-gateway is available: 0.1.28 → 0.1.29. `+
		`Run "rack-gateway update" to install it.`, notice)
}

// A build no release can replace is told to build the new release from source instead.
func TestNoticeForBuildsNoReleaseReplaces(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, latestHandler(t, &requests, "v0.1.29"))
	sourceBuild := "A new release of rack-gateway is available: 0.1.28 → 0.1.29. Build it from source: " +
		client.DownloadBaseURL + "/DocSpring/rack-gateway/releases/tag/v0.1.29"

	for name, modify := range map[string]func(c *Checker){
		"linux with security keys": func(c *Checker) { c.Current.SecurityKeys = true },
		"intel mac":                func(c *Checker) { c.GOOS = "darwin" },
	} {
		t.Run(name, func(t *testing.T) {
			checker := (&memoryState{}).checker(client, "0.1.28")
			modify(checker)
			require.Equal(t, sourceBuild, checker.Start().Notice(5*time.Second))
		})
	}

	macWithKeys := (&memoryState{}).checker(client, "0.1.28")
	macWithKeys.GOOS, macWithKeys.GOARCH, macWithKeys.Current.SecurityKeys = "darwin", "arm64", true
	requireNotice(t, macWithKeys.Start().Notice(5*time.Second))
}

func TestCheckAsksGitHubOnceADay(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, latestHandler(t, &requests, "v0.1.29"))
	state := &memoryState{}

	notice := state.checker(client, "0.1.28").Start().Notice(5 * time.Second)
	requireNotice(t, notice)
	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, checkState{CheckedAt: testNow, LatestTag: "v0.1.29"}, state.get(t))

	// Within a day, the saved result is used without asking GitHub.
	notice = state.checker(client, "0.1.28").Start().Notice(5 * time.Second)
	requireNotice(t, notice)
	require.Equal(t, int32(1), requests.Load())

	// After a day, it asks again.
	state.set(t, checkState{CheckedAt: testNow.Add(-CheckInterval), LatestTag: "v0.1.29"})
	require.Empty(t, state.checker(client, "0.1.29").Start().Notice(5*time.Second))
	require.Equal(t, int32(2), requests.Load())
}

func TestCheckShowsNothingWhenUpToDate(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, latestHandler(t, &requests, "v0.1.28"))

	require.Empty(t, (&memoryState{}).checker(client, "0.1.28").Start().Notice(5*time.Second))
	require.Empty(t, (&memoryState{}).checker(client, "0.1.29").Start().Notice(5*time.Second))

	// A saved tag that isn't a release is ignored.
	state := &memoryState{}
	state.set(t, checkState{CheckedAt: testNow, LatestTag: "v9.9.9\u001b[2J"})
	require.Empty(t, state.checker(client, "0.1.28").Start().Notice(5*time.Second))
}

func TestFailedCheckIsSavedAndKeepsTheLastResult(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	state := &memoryState{}
	state.set(t, checkState{CheckedAt: testNow.Add(-2 * CheckInterval), LatestTag: "v0.1.29"})

	notice := state.checker(client, "0.1.28").Start().Notice(5 * time.Second)
	requireNotice(t, notice)
	require.Equal(t, checkState{CheckedAt: testNow, LatestTag: "v0.1.29"}, state.get(t))
}

func TestCheckStillRunningAfterTheCommandIsCanceled(t *testing.T) {
	release := make(chan struct{})
	client := testClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release) })
	state := &memoryState{}
	saved := checkState{CheckedAt: testNow.Add(-2 * CheckInterval), LatestTag: "v0.1.29"}
	state.set(t, saved)

	check := state.checker(client, "0.1.28").Start()
	requireNotice(t, check.Notice(10*time.Millisecond))
	<-check.done
	require.Zero(t, state.saves.Load(), "a canceled check isn't saved, so the next command checks again")
	require.Equal(t, saved, state.get(t))
}
