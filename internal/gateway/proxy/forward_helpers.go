package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// prepareProxyRequest creates a new HTTP request for proxying to the Convox rack.
func prepareProxyRequest(
	r *http.Request,
	targetURL string,
	bodyBytes []byte,
	rack config.RackConfig,
	authUser *auth.User,
) (*http.Request, error) {
	// Validate targetURL is to configured rack to prevent SSRF
	if !strings.HasPrefix(targetURL, rack.URL) {
		return nil, fmt.Errorf("target URL does not match configured rack")
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	copyForwardedHeaders(proxyReq.Header, r.Header)
	setGatewayHeaders(proxyReq.Header, rack, authUser)

	return proxyReq, nil
}

// readRequestBody reads and closes the request body, returning the bytes.
func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if err := r.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close request body: %w", err)
	}

	return bodyBytes, nil
}
