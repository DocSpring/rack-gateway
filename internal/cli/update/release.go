// Package update finds new rack-gateway CLI releases on GitHub, verifies them and installs them.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Repository is the GitHub repository the CLI's releases come from. The Release workflow sets it to the repository
// that builds the release, so a fork's CLI updates from the fork.
var Repository = "DocSpring/rack-gateway"

const (
	githubAPIBaseURL      = "https://api.github.com"
	githubDownloadBaseURL = "https://github.com"
	maxRedirects          = 5
	maxAPIResponseBytes   = 1 << 20
)

// releaseTagPattern is the tag format the Release workflow accepts.
var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// Release is a published GitHub release of the CLI.
type Release struct {
	Tag string `json:"tag_name"`
}

// Version is the release's version without the "v" prefix, as `rack-gateway version` prints it.
func (r Release) Version() string {
	return strings.TrimPrefix(r.Tag, "v")
}

// Client reads releases from GitHub and downloads their assets.
type Client struct {
	HTTP            *http.Client
	APIBaseURL      string
	DownloadBaseURL string
	Repository      string
	UserAgent       string
}

// NewClient returns a client for this CLI's GitHub releases. Requests and redirects may only go to GitHub over
// HTTPS.
func NewClient(currentVersion string, timeout time.Duration) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout:       timeout,
			CheckRedirect: checkRedirect(isGitHubURL),
		},
		APIBaseURL:      githubAPIBaseURL,
		DownloadBaseURL: githubDownloadBaseURL,
		Repository:      Repository,
		UserAgent:       "rack-gateway/" + currentVersion,
	}
}

// isGitHubURL reports whether u is an HTTPS URL on GitHub or the hosts GitHub serves release assets from.
func isGitHubURL(u *url.URL) bool {
	host := u.Hostname()
	return u.Scheme == "https" &&
		(host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com"))
}

func checkRedirect(allowed func(*url.URL) bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		if !allowed(req.URL) {
			return fmt.Errorf("refusing to follow a redirect to %s", req.URL.Redacted())
		}
		return nil
	}
}

// LatestRelease returns the newest stable release.
func (c *Client) LatestRelease(ctx context.Context) (*Release, error) {
	return c.release(ctx, "releases/latest")
}

// ReleaseByTag returns the published release with this tag.
func (c *Client) ReleaseByTag(ctx context.Context, tag string) (*Release, error) {
	if !releaseTagPattern.MatchString(tag) {
		return nil, fmt.Errorf("invalid release tag %q (expected vMAJOR.MINOR.PATCH)", tag)
	}
	return c.release(ctx, "releases/tags/"+tag)
}

func (c *Client) release(ctx context.Context, path string) (*Release, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s", c.APIBaseURL, c.Repository, path)
	body, err := c.get(ctx, endpoint, maxAPIResponseBytes, map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	})
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("failed to parse the GitHub release: %w", err)
	}
	if !releaseTagPattern.MatchString(rel.Tag) {
		return nil, fmt.Errorf("GitHub returned an invalid release tag %q", rel.Tag)
	}
	return &rel, nil
}

// ReleaseURL returns the web page of the release with this tag.
func (c *Client) ReleaseURL(tag string) string {
	return fmt.Sprintf("%s/%s/releases/tag/%s", c.DownloadBaseURL, c.Repository, url.PathEscape(tag))
}

// Download returns one of a release's assets, refusing anything larger than limit bytes.
func (c *Client) Download(ctx context.Context, tag, asset string, limit int64) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/%s/releases/download/%s/%s",
		c.DownloadBaseURL, c.Repository, url.PathEscape(tag), url.PathEscape(asset))
	body, err := c.get(ctx, endpoint, limit, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", asset, err)
	}
	return body, nil
}

func (c *Client) get(ctx context.Context, endpoint string, limit int64, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: response is larger than %d bytes", endpoint, limit)
	}
	return body, nil
}

// IsReleaseVersion reports whether version is a release version ("0.1.29" or "v0.1.29"), not a development build.
func IsReleaseVersion(version string) bool {
	return releaseTagPattern.MatchString(TagFor(version))
}

// IsNewer reports whether release version a is newer than release version b.
func IsNewer(a, b string) bool {
	return semver.Compare(TagFor(a), TagFor(b)) > 0
}

// TagFor returns the release tag for a version, with or without its "v" prefix.
func TagFor(version string) string {
	return "v" + strings.TrimPrefix(version, "v")
}

// Replaceable returns why no release can replace a build for this platform, or nil when one can. Releases have
// builds for darwin/arm64, linux/amd64 and linux/arm64, and only the macOS one supports security keys, which a
// build that supports them must keep.
func Replaceable(goos, goarch string, securityKeys bool) error {
	if _, ok := AssetName(goos, goarch); !ok {
		return fmt.Errorf("releases have no build for %s/%s: build the CLI from source", goos, goarch)
	}
	if securityKeys && goos != "darwin" {
		return fmt.Errorf("the %s/%s release has no security key support, and this build has it: "+
			"build the CLI from source to keep it", goos, goarch)
	}
	return nil
}

// AssetName returns the name of the release archive for a platform, and whether releases include one.
func AssetName(goos, goarch string) (string, bool) {
	switch goos + "/" + goarch {
	case "darwin/arm64", "linux/amd64", "linux/arm64":
		return fmt.Sprintf("rack-gateway-%s-%s.tar.gz", goos, goarch), true
	default:
		return "", false
	}
}
