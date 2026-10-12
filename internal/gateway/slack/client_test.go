package slack

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// roundTripFunc answers a client's requests without a network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// A message can link to a URL from CI, so Slack mustn't show a preview of whatever that page chooses.
func TestPostMessageTurnsOffUnfurling(t *testing.T) {
	var requestURL string
	var payload map[string]interface{}
	client := NewClient(fakeSlackBot)
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestURL = req.URL.String()
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})}

	text, blocks := TestMessage(testGateway)
	require.NoError(t, client.PostMessage("C123", text, blocks))

	require.Equal(t, slackAPIBase+"/chat.postMessage", requestURL)
	require.Equal(t, "C123", payload["channel"])
	require.Equal(t, false, payload["unfurl_links"])
	require.Equal(t, false, payload["unfurl_media"])
}
