package proxy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
)

// prepareProxyRequest creates a new HTTP request for proxying to the Convox rack.
func prepareProxyRequest(
	r *http.Request,
	targetURL string,
	bodyBytes []byte,
	rack config.RackConfig,
	authUser *auth.User,
) (*http.Request, error) {
	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	httputil.CopyHeaders(
		proxyReq.Header,
		r.Header,
		"authorization",
		"env",
		"environment",
		"release-env",
		"x-audit-resource",
	)

	proxyReq.Header.Set("Authorization", fmt.Sprintf("Basic %s",
		base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	proxyReq.Header.Set("X-User-Email", authUser.Email)
	proxyReq.Header.Set("X-Request-ID", uuid.New().String())

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

// buildTargetURL constructs the full target URL for the proxied request.
func buildTargetURL(rack config.RackConfig, path string, rawQuery string) string {
	base := strings.TrimRight(rack.URL, "/")
	p := "/" + strings.TrimLeft(path, "/")
	targetURL := base + p
	if rawQuery != "" {
		targetURL += "?" + rawQuery
	}
	return targetURL
}

// shouldBufferResponse determines whether the response needs to be buffered.
func shouldBufferResponse(r *http.Request, pth string, isJSON bool) (bool, bool, bool) {
	filterRelease := isJSON && (strings.Contains(pth, "/apps/") && strings.Contains(pth, "/releases"))
	shouldCapture := false
	captureProcess := false

	if isJSON {
		switch r.Method {
		case http.MethodPost:
			shouldCapture = true
			if strings.Contains(pth, "/services/") && strings.Contains(pth, "/processes") {
				captureProcess = true
			}
		case http.MethodGet:
			if (strings.Contains(pth, "/builds/") || strings.Contains(pth, "/releases/")) &&
				!strings.HasSuffix(pth, "/builds") && !strings.HasSuffix(pth, "/releases") {
				shouldCapture = true
			}
		}
	}

	needsBuffer := filterRelease || shouldCapture || captureProcess
	return needsBuffer, filterRelease, shouldCapture
}
