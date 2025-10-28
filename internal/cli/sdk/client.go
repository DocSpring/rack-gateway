package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	clilog "github.com/DocSpring/rack-gateway/internal/cli/logging"
	"github.com/convox/convox/pkg/structs"
	"github.com/convox/stdsdk"
)

const maxLoggedBodyBytes = 4096

// Client is a custom SDK client that talks to the rack-gateway API.
type Client struct {
	gatewayURL string
	token      string
	httpClient *http.Client
}

// New creates a new gateway SDK client.
func New(gatewayURL, token string) *Client {
	return &Client{
		gatewayURL: gatewayURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// request makes an authenticated HTTP request to the gateway API.
func (c *Client) request(method, path string, body interface{}, out interface{}) error {
	fullURL := c.gatewayURL + "/api/v1/rack-proxy" + path

	var payload io.Reader
	var requestBody []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
		requestBody = data
	}

	req, err := http.NewRequest(method, fullURL, payload)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if clilog.TopicEnabled(clilog.TopicHTTP) {
		clilog.DebugTopicf(clilog.TopicHTTP, "%s %s", method, fullURL)
	}
	if clilog.TopicEnabled(clilog.TopicHTTPBody) && len(requestBody) > 0 {
		clilog.DebugTopicf(clilog.TopicHTTPBody, "payload=%s", truncateBody(requestBody))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	if clilog.TopicEnabled(clilog.TopicHTTP) {
		clilog.DebugTopicf(clilog.TopicHTTP, "<-- %d %s", resp.StatusCode, fullURL)
	}
	if clilog.TopicEnabled(clilog.TopicHTTPBody) && len(respBody) > 0 {
		clilog.DebugTopicf(clilog.TopicHTTPBody, "response=%s", truncateBody(respBody))
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("gateway request failed (%d): %s", resp.StatusCode, string(respBody))
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}

	return json.Unmarshal(respBody, out)
}

func truncateBody(body []byte) string {
	if len(body) <= maxLoggedBodyBytes {
		return string(body)
	}
	return string(body[:maxLoggedBodyBytes]) + "…(truncated)"
}

// ProcessList lists processes for an app.
func (c *Client) ProcessList(app string, opts structs.ProcessListOptions) (structs.Processes, error) {
	var processes structs.Processes

	path := fmt.Sprintf("/apps/%s/processes", app)
	if err := c.request(http.MethodGet, path, nil, &processes); err != nil {
		return nil, err
	}

	return processes, nil
}

// Get makes a raw GET request (required by sdk.Interface).
func (c *Client) Get(path string, opts stdsdk.RequestOptions, out interface{}) error {
	return c.request(http.MethodGet, path, nil, out)
}

// Endpoint returns the gateway URL (required by sdk.Interface).
func (c *Client) Endpoint() (*url.URL, error) {
	return url.Parse(c.gatewayURL + "/api/v1/rack-proxy")
}

// ClientType returns the client type (required by sdk.Interface).
func (c *Client) ClientType() string {
	return "standard"
}

// WithContext returns a copy of the client with the given context (required by sdk.Interface).
func (c *Client) WithContext(ctx context.Context) structs.Provider {
	return c
}

// Workers returns an error (required by structs.Provider).
func (c *Client) Workers() error {
	return nil
}
