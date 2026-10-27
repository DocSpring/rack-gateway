package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testClient returns a client whose API and downloads are served by handler over plain HTTP.
func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient("0.1.28", 5*time.Second)
	client.HTTP.CheckRedirect = nil
	client.APIBaseURL = server.URL
	client.DownloadBaseURL = server.URL
	client.Repository = testRepository
	return client
}

func TestLatestRelease(t *testing.T) {
	var gotPath, gotAgent string
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAgent = r.URL.Path, r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.29","html_url":"https://example.com"}`))
	}))

	rel, err := client.LatestRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v0.1.29", rel.Tag)
	require.Equal(t, "0.1.29", rel.Version())
	require.Equal(t, "/repos/DocSpring/rack-gateway/releases/latest", gotPath)
	require.Equal(t, "rack-gateway/0.1.28", gotAgent)
}

func TestReleaseByTag(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/DocSpring/rack-gateway/releases/tags/v0.1.27", r.URL.Path)
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.27"}`))
	}))

	rel, err := client.ReleaseByTag(context.Background(), "v0.1.27")
	require.NoError(t, err)
	require.Equal(t, &Release{Tag: "v0.1.27"}, rel)
}

func TestReleaseRejectsBadTagsAndErrors(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/DocSpring/rack-gateway/releases/tags/v0.1.29":
			_, _ = w.Write([]byte(`{"tag_name":"v0.1.29\u001b[2J"}`))
		default:
			http.Error(w, "rate limited", http.StatusForbidden)
		}
	}))

	_, err := client.ReleaseByTag(context.Background(), "v0.1.29")
	require.ErrorContains(t, err, "invalid release tag")
	_, err = client.ReleaseByTag(context.Background(), "../../evil")
	require.ErrorContains(t, err, `invalid release tag "../../evil"`)
	_, err = client.LatestRelease(context.Background())
	require.ErrorContains(t, err, "403 Forbidden")
}

func TestDownloadEnforcesTheSizeLimit(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/DocSpring/rack-gateway/releases/download/v0.1.29/asset.tar.gz", r.URL.Path)
		_, _ = w.Write([]byte(strings.Repeat("x", 10)))
	}))

	body, err := client.Download(context.Background(), "v0.1.29", "asset.tar.gz", 10)
	require.NoError(t, err)
	require.Len(t, body, 10)
	_, err = client.Download(context.Background(), "v0.1.29", "asset.tar.gz", 9)
	require.ErrorContains(t, err, "larger than 9 bytes")
}

func TestRedirectsStayOnGitHub(t *testing.T) {
	check := NewClient("0.1.28", time.Second).HTTP.CheckRedirect
	redirect := func(rawURL string, hops int) error {
		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		return check(&http.Request{URL: u}, make([]*http.Request, hops))
	}

	require.NoError(t, redirect("https://release-assets.githubusercontent.com/github-production-release-asset/1", 1))
	require.NoError(t, redirect("https://objects.githubusercontent.com/github-production-release-asset-2e65be/1", 1))
	require.NoError(t, redirect("https://github.com/DocSpring/rack-gateway/releases/download/v0.1.29/a", 1))
	require.ErrorContains(t, redirect("http://objects.githubusercontent.com/a", 1), "refusing to follow a redirect")
	require.ErrorContains(t, redirect("https://example.com/a", 1), "refusing to follow a redirect")
	require.ErrorContains(t, redirect("https://githubusercontent.com.example.com/a", 1), "refusing to follow")
	require.ErrorContains(t, redirect("https://github.com/a", maxRedirects), "too many redirects")
}

func TestVersions(t *testing.T) {
	require.True(t, IsReleaseVersion("0.1.29"))
	require.True(t, IsReleaseVersion("v0.1.29"))
	require.True(t, IsReleaseVersion("0.2.0-rc.1"))
	require.False(t, IsReleaseVersion("dev"))
	require.False(t, IsReleaseVersion("0.1"))
	require.False(t, IsReleaseVersion("0.1.29 "))

	require.True(t, IsNewer("v0.1.29", "0.1.28"))
	require.True(t, IsNewer("0.1.10", "0.1.9"))
	require.True(t, IsNewer("0.2.0", "0.2.0-rc.1"))
	require.False(t, IsNewer("0.1.28", "0.1.28"))
	require.False(t, IsNewer("0.1.27", "0.1.28"))
	require.False(t, IsNewer("garbage", "0.1.28"))
}

func TestAssetName(t *testing.T) {
	for _, platform := range [][2]string{{"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}} {
		name, ok := AssetName(platform[0], platform[1])
		require.True(t, ok)
		require.Equal(t, "rack-gateway-"+platform[0]+"-"+platform[1]+".tar.gz", name)
	}
	for _, platform := range [][2]string{{"darwin", "amd64"}, {"windows", "amd64"}, {"freebsd", "arm64"}} {
		_, ok := AssetName(platform[0], platform[1])
		require.False(t, ok)
	}
}
