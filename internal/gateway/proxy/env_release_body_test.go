package proxy

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// withBody replaces a release create's body.
func withBody(req *http.Request, body, contentType string) *http.Request {
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", contentType)
	return req
}

// An empty env is still the release's whole env on the rack, so it goes through the env checks: protected keys
// are kept, and only keys the caller may remove are removed.
func TestEmptyEnvReleaseKeepsProtectedKeys(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, "", "", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	releases := f.rack.releases()
	require.Len(t, releases, 1)
	require.Contains(t, releases[0].env, "DATABASE_URL=postgres://old")
	require.Contains(t, releases[0].env, "ADMIN_PASSWORD=hunter2")
	require.NotContains(t, releases[0].env, "PORT=")
}

// Without an env, parent-release would give the new release an older release's env (for example a protected
// value from before a rotation), unchecked.
func TestParentReleaseWithoutEnvIsRefused(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, "", "", "", stepUpAgeFresh)
	body := url.Values{"parent-release": {"R0"}}.Encode()
	rr := f.proxy(withBody(req, body, "application/x-www-form-urlencoded"))
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "parent-release needs an env")
	require.Empty(t, f.rack.releases())
}

// The rack parses fields out of other body types too, so only form-encoded release requests are forwarded.
func TestReleaseCreateMustBeFormEncoded(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, "", "", "", stepUpAgeFresh)
	body := "--b\r\nContent-Disposition: form-data; name=\"env\"\r\n\r\n\r\n--b--\r\n"
	rr := f.proxy(withBody(req, body, "multipart/form-data; boundary=b"))
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Empty(t, f.rack.releases())
}
