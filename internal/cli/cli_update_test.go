package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/cli/update"
	"github.com/DocSpring/rack-gateway/internal/cli/webauthn"
)

func setVersion(t *testing.T, version string) {
	t.Helper()
	previous := Version
	Version = version
	t.Cleanup(func() { Version = previous })
}

// releaseClient returns a release client whose GitHub API serves the latest release and release v0.1.27.
func releaseClient(t *testing.T, latest string) *update.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/DocSpring/rack-gateway/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"` + latest + `"}`))
		case "/repos/DocSpring/rack-gateway/releases/tags/v0.1.27":
			_, _ = w.Write([]byte(`{"tag_name":"v0.1.27"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := update.NewClient("test", 5*time.Second)
	client.APIBaseURL = server.URL
	client.DownloadBaseURL = "https://github.com"
	client.Repository = "DocSpring/rack-gateway"
	return client
}

func runUpdateForTest(t *testing.T, client *update.Client, checkOnly bool, version string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	err := runUpdate(context.Background(), cmd, client, checkOnly, version)
	return out.String(), err
}

func TestUpdateWhenAlreadyCurrentOrNewer(t *testing.T) {
	setVersion(t, "0.1.28")

	out, err := runUpdateForTest(t, releaseClient(t, "v0.1.28"), false, "")
	require.NoError(t, err)
	require.Equal(t, "rack-gateway 0.1.28 is the latest release.\n", out)

	out, err = runUpdateForTest(t, releaseClient(t, "v0.1.27"), false, "")
	require.NoError(t, err)
	require.Equal(t, "rack-gateway 0.1.28 is newer than the latest release (0.1.27).\n", out)
}

func TestUpdateCheckOnlyReportsTheRelease(t *testing.T) {
	setVersion(t, "0.1.28")

	out, err := runUpdateForTest(t, releaseClient(t, "v0.1.29"), true, "")
	require.NoError(t, err)
	require.Equal(t, "rack-gateway 0.1.29 is available (this is 0.1.28): "+
		"https://github.com/DocSpring/rack-gateway/releases/tag/v0.1.29\n"+
		"Run \"rack-gateway update\" to install it.\n", out)

	// --version names a release even when it's older than this one.
	out, err = runUpdateForTest(t, releaseClient(t, "v0.1.29"), true, "0.1.27")
	require.NoError(t, err)
	require.Contains(t, out, "rack-gateway 0.1.27 is available (this is 0.1.28)")
}

func TestUpdateRejectsDevBuildsAndBadVersions(t *testing.T) {
	setVersion(t, "dev")
	_, err := runUpdateForTest(t, releaseClient(t, "v0.1.29"), false, "")
	require.ErrorContains(t, err, "this is a development build (dev)")

	setVersion(t, "0.1.28")
	_, err = runUpdateForTest(t, releaseClient(t, "v0.1.29"), false, "latest")
	require.ErrorContains(t, err, `invalid --version "latest"`)
	_, err = runUpdateForTest(t, releaseClient(t, "v0.1.29"), false, "v0.1.99")
	require.ErrorContains(t, err, "failed to find release v0.1.99")
}

func TestUpdateCheckEnabled(t *testing.T) {
	root := &cobra.Command{Use: "rack-gateway"}
	apps := &cobra.Command{Use: "apps"}
	completion := &cobra.Command{Use: "completion"}
	zsh := &cobra.Command{Use: "zsh"}
	completion.AddCommand(zsh)
	root.AddCommand(apps, completion, UpdateCommand(), VersionCommand(), BuildInfoCommand())
	t.Setenv("CI", "")
	t.Setenv("RACK_GATEWAY_NO_UPDATE_CHECK", "")

	setVersion(t, "0.1.28")
	require.True(t, updateCheckEnabled(apps))
	require.True(t, updateCheckEnabled(root))
	for _, name := range []string{"update", "version", "build-info"} {
		cmd, _, err := root.Find([]string{name})
		require.NoError(t, err)
		require.False(t, updateCheckEnabled(cmd), name)
	}
	require.False(t, updateCheckEnabled(zsh))

	t.Setenv("CI", "true")
	require.False(t, updateCheckEnabled(apps))
	t.Setenv("CI", "")
	t.Setenv("RACK_GATEWAY_NO_UPDATE_CHECK", "1")
	require.False(t, updateCheckEnabled(apps))
	t.Setenv("RACK_GATEWAY_NO_UPDATE_CHECK", "")
	setVersion(t, "dev")
	require.False(t, updateCheckEnabled(apps))
}

func TestBuildInfoCommand(t *testing.T) {
	setVersion(t, "0.1.28")
	var out bytes.Buffer
	cmd := BuildInfoCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	info, err := update.ParseBuildInfo(out.Bytes())
	require.NoError(t, err)
	require.Equal(t, update.BuildInfo{Version: "0.1.28", SecurityKeys: webauthn.Supported}, info)
}
