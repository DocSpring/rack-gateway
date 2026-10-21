package envutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// fakeReleasesRack answers the latest release list and release R1 with the given statuses and bodies.
func fakeReleasesRack(t *testing.T, listStatus int, list string, releaseStatus int, release string) config.RackConfig {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apps/myapp/releases":
			w.WriteHeader(listStatus)
			_, _ = w.Write([]byte(list))
		case "/apps/myapp/releases/R1":
			w.WriteHeader(releaseStatus)
			_, _ = w.Write([]byte(release))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return config.RackConfig{URL: server.URL, Username: "convox", APIKey: "key"}
}

func TestFetchLatestEnvMapReadsTheLatestRelease(t *testing.T) {
	rack := fakeReleasesRack(t, http.StatusOK, `[{"id":"R1"}]`, http.StatusOK, `{"env":"FOO=bar\nEMPTY=\nURL=a=b"}`)
	env, err := FetchLatestEnvMap(context.Background(), rack, "myapp", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"FOO": "bar", "EMPTY": "", "URL": "a=b"}, env)
}

// An app without a release, or whose latest release has no variables, has an empty env.
func TestFetchLatestEnvMapReturnsAnEmptyEnvOnlyForAnEmptyApp(t *testing.T) {
	rack := fakeReleasesRack(t, http.StatusOK, `[]`, http.StatusOK, `{}`)
	env, err := FetchLatestEnvMap(context.Background(), rack, "myapp", nil)
	require.NoError(t, err)
	require.Empty(t, env)

	rack = fakeReleasesRack(t, http.StatusOK, `[{"id":"R1"}]`, http.StatusOK, `{"env":""}`)
	env, err = FetchLatestEnvMap(context.Background(), rack, "myapp", nil)
	require.NoError(t, err)
	require.Empty(t, env)
}

// Callers write the whole env back, so a failed read must never look like an empty env: that would remove every
// variable the change didn't name.
func TestFetchLatestEnvMapFailsWhenTheRackDoesntAnswerWithARelease(t *testing.T) {
	cases := map[string]config.RackConfig{
		"list error":        fakeReleasesRack(t, http.StatusInternalServerError, `{"error":"x"}`, http.StatusOK, `{}`),
		"list unauthorized": fakeReleasesRack(t, http.StatusUnauthorized, `[]`, http.StatusOK, `{}`),
		"release missing":   fakeReleasesRack(t, http.StatusOK, `[{"id":"R1"}]`, http.StatusNotFound, `{}`),
		"release error":     fakeReleasesRack(t, http.StatusOK, `[{"id":"R1"}]`, http.StatusBadGateway, `{"env":""}`),
		"release no env":    fakeReleasesRack(t, http.StatusOK, `[{"id":"R1"}]`, http.StatusOK, `{"id":"R1"}`),
		"release no ID":     fakeReleasesRack(t, http.StatusOK, `[{"id":""}]`, http.StatusOK, `{"env":""}`),
		"not JSON":          fakeReleasesRack(t, http.StatusOK, `<html>`, http.StatusOK, `{}`),
	}
	for name, rack := range cases {
		env, err := FetchLatestEnvMap(context.Background(), rack, "myapp", nil)
		require.Error(t, err, name)
		require.Nil(t, env, name)
	}
}
