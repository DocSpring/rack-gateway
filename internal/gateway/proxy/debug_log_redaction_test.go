package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/debugredact"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/debuglog"
)

// rackResponse is a rack response to a request for path, as the proxy logs it.
func rackResponse(t *testing.T, path string, header http.Header) *http.Response {
	t.Helper()
	upstream, err := url.Parse("https://convox:hidden-rack-password@rack.example.com" + path + "?token=hidden-token")
	require.NoError(t, err)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Request:    &http.Request{Method: http.MethodGet, URL: upstream},
	}
}

// logRackResponse logs a rack response with every debug topic on, as processProxyResponse does, with the start
// of its body that the proxy keeps for the log.
func logRackResponse(t *testing.T, path string, resp *http.Response, body string) {
	t.Helper()
	h := &Handler{}
	h.logResponseHeaders(resp)
	snippet := httputil.TruncateBytes([]byte(body), proxyLogBodyLimit)
	h.logProxyResponse(httptest.NewRequest(http.MethodGet, path, nil), resp, path, "application/json",
		int64(len(body)), snippet, true, true, true)
}

// Debug logs of rack responses must not carry the rack's secrets: its password, resource credentials, env
// values, or a redirect's codes.
func TestRackResponseDebugLogsAreRedacted(t *testing.T) {
	logs := debuglog.Capture(t, "http.*,proxy")

	resources := rackResponse(t, "/apps/web/resources", http.Header{
		"Content-Type": {"application/json"},
		"Location":     {"https://gateway.example.com/callback?code=hidden-code&state=hidden-state"},
		"Set-Cookie":   {"session=hidden-cookie"},
	})
	logRackResponse(t, "/apps/web/resources", resources,
		`[{"name":"db","type":"postgres","url":"postgres://app:hidden-db-password@db.internal:5432/app"}]`)

	environment := rackResponse(t, "/apps/web/environment", http.Header{"Content-Type": {"application/json"}})
	logRackResponse(t, "/apps/web/environment", environment,
		`{"RAILS_ENV":"hidden-env-value","SECRET_KEY_BASE":"hidden-secret-value"}`)

	releases := rackResponse(t, "/apps/web/releases", http.Header{"Content-Type": {"application/json"}})
	// Longer than the proxy keeps for the log, so its start can't be parsed or redacted.
	logRackResponse(t, "/apps/web/releases", releases,
		`[{"id":"RABC","env":"DATABASE_URL=postgres://hidden-release-env","manifest":"`+
			strings.Repeat("x", proxyLogBodyLimit)+`"}]`)

	out := logs.String()
	for _, logged := range []string{
		"upstream_url=\"https://rack.example.com/apps/web/resources?token=%5BREDACTED%5D\"",
		"Location: https://gateway.example.com/callback?code=%5BREDACTED%5D&state=%5BREDACTED%5D",
		"Set-Cookie: [REDACTED]",
		`"name":"db"`,
		`"url":"[REDACTED]"`,
		`"RAILS_ENV":"[REDACTED]"`,
		debugredact.TooLongPlaceholder,
	} {
		require.Contains(t, out, logged)
	}
	require.NotContains(t, out, "hidden-")
}
