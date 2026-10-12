package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// maxRackJSONBytes bounds a rack response the gateway reads for its own checks. A release carries the app's
	// whole env and its manifest (at most maxManifestSize), so even a large one is well under this.
	maxRackJSONBytes = 4 << 20
	// rackFetchTimeout bounds each of those reads.
	rackFetchTimeout = 30 * time.Second
)

// rackRelease is what the gateway reads of a Convox release (structs.Release).
type rackRelease struct {
	ID       string `json:"id"`
	Build    string `json:"build"`
	Env      string `json:"env"`
	Manifest string `json:"manifest"`
}

// rackBuild is what the gateway reads of a Convox build (structs.Build).
type rackBuild struct {
	ID     string `json:"id"`
	GitSha string `json:"git-sha"`
}

// fetchRelease reads one of the app's releases from the rack.
func (h *Handler) fetchRelease(ctx context.Context, app, id string) (*rackRelease, error) {
	var release rackRelease
	if err := h.fetchRackJSON(ctx, url.Values{}, &release, "apps", app, "releases", id); err != nil {
		return nil, err
	}
	return &release, nil
}

// fetchBuild reads one of the app's builds from the rack.
func (h *Handler) fetchBuild(ctx context.Context, app, id string) (*rackBuild, error) {
	var build rackBuild
	if err := h.fetchRackJSON(ctx, url.Values{}, &build, "apps", app, "builds", id); err != nil {
		return nil, err
	}
	return &build, nil
}

// fetchLatestReleaseEnv reads the env of the app's latest release, which is the env the rack gives a build
// (Convox common.AppEnvironment). An app without releases has none.
func (h *Handler) fetchLatestReleaseEnv(ctx context.Context, app string) (map[string]string, error) {
	var releases []rackRelease
	if err := h.fetchRackJSON(ctx, url.Values{"limit": {"1"}}, &releases, "apps", app, "releases"); err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return map[string]string{}, nil
	}
	release, err := h.fetchRelease(ctx, app, releases[0].ID)
	if err != nil {
		return nil, err
	}
	return parseRackEnv(release.Env)
}

// fetchRackJSON GETs the rack API path made of segments (each escaped), with query, and decodes the JSON response
// into out. Anything but a 200 is an error.
func (h *Handler) fetchRackJSON(ctx context.Context, query url.Values, out interface{}, segments ...string) error {
	rack, err := h.getRackConfig()
	if err != nil {
		return err
	}
	escaped, err := escapePathSegments(segments)
	if err != nil {
		return err
	}
	target, err := url.JoinPath(rack.URL, escaped...)
	if err != nil {
		return fmt.Errorf("invalid rack URL: %w", err)
	}
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("failed to create rack request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte(rack.Username+":"+rack.APIKey)))

	client, err := h.httpClient(ctx, rackFetchTimeout)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to read %s from the rack: %w", req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reading %s from the rack failed with status %d", req.URL.Path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRackJSONBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read %s from the rack: %w", req.URL.Path, err)
	}
	if len(body) > maxRackJSONBytes {
		return fmt.Errorf("%s from the rack is larger than %d bytes", req.URL.Path, maxRackJSONBytes)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode %s from the rack: %w", req.URL.Path, err)
	}
	return nil
}

// escapePathSegments escapes each segment of a rack path. url.JoinPath would resolve a "." or ".." segment, so
// those (and empty ones) are refused rather than escaped.
func escapePathSegments(segments []string) ([]string, error) {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil, fmt.Errorf("invalid rack path segment %q", segment)
		}
		escaped[i] = url.PathEscape(segment)
	}
	return escaped, nil
}
