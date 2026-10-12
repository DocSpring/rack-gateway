package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/debugredact"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// processProxyResponse handles the response from the proxied request.
func (h *Handler) processProxyResponse(
	w http.ResponseWriter,
	r *http.Request,
	resp *http.Response,
	path string,
	authUserEmail string,
) (int, error) {
	handling := h.responseHandlingFor(r, resp, path)
	needsBuffer := handling.needsBuffer()

	logProxy := gtwlog.TopicEnabled(gtwlog.TopicProxy)
	logResponse := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponse)
	logResponseHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
	logResponseBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

	var body []byte
	var respReader io.Reader
	var err error

	if needsBuffer {
		body, err = h.processBufferedResponse(r, resp, path, authUserEmail, handling)
		if err != nil {
			return 0, err
		}
		respReader = bytes.NewReader(body)
	} else {
		respReader = resp.Body
	}

	h.markDeployApprovalDeployedAfterPromote(r, path, resp.StatusCode)

	httputil.CopyHeaders(w.Header(), resp.Header, "content-length")
	w.WriteHeader(resp.StatusCode)

	contentType := resp.Header.Get("Content-Type")
	shouldCaptureBody := logResponseBody && !httputil.IsBinaryContent(contentType)

	bytesWritten, logSnippet, err := h.writeResponse(
		w,
		respReader,
		body,
		needsBuffer,
		shouldCaptureBody,
	)
	if err != nil {
		return resp.StatusCode, err
	}

	if logResponseHeaders {
		h.logResponseHeaders(resp)
	}

	h.logProxyResponse(
		r,
		resp,
		path,
		contentType,
		bytesWritten,
		logSnippet,
		logProxy,
		logResponse,
		logResponseBody,
	)

	return resp.StatusCode, nil
}

// responseHandling is what the gateway does with a rack response before passing it on. Any of it needs the
// response read into memory first.
type responseHandling struct {
	filterRelease     bool // mask secret env values in releases
	filterEnvironment bool // mask secret env values in GET /apps/{app}/environment
	filterBuild       bool // strip build repository URLs (see stripBuildRepository)
	capture           bool // record who created the resource
	captureProcess    bool // track a process started under a deploy approval
}

func (rh responseHandling) needsBuffer() bool {
	return rh.filterRelease || rh.filterEnvironment || rh.filterBuild || rh.capture || rh.captureProcess
}

func (h *Handler) responseHandlingFor(r *http.Request, resp *http.Response, path string) responseHandling {
	isJSON := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json")
	handling := responseHandling{
		filterRelease: isJSON &&
			(rbac.KeyMatch3(path, "/apps/{app}/releases") || rbac.KeyMatch3(path, "/apps/{app}/releases/{id}")),
		filterEnvironment: isJSON && isEnvironmentRead(r, path),
		// Build create, import, update, get and list all answer with builds.
		filterBuild: isJSON &&
			(rbac.KeyMatch3(path, "/apps/{app}/builds") || rbac.KeyMatch3(path, "/apps/{app}/builds/{id}")),
	}
	handling.capture, handling.captureProcess = h.shouldCaptureResponse(r, path, isJSON)
	return handling
}

// shouldCaptureResponse determines if the response should be captured.
func (_ *Handler) shouldCaptureResponse(r *http.Request, path string, isJSON bool) (bool, bool) {
	if !isJSON {
		return false, false
	}

	shouldCapture := false
	captureProcess := false

	switch r.Method {
	case http.MethodPost:
		shouldCapture = true
		if rbac.KeyMatch3(path, "/apps/{app}/services/{service}/processes") {
			captureProcess = true
		}
	case http.MethodGet:
		if rbac.KeyMatch3(path, "/apps/{app}/builds/{id}") ||
			rbac.KeyMatch3(path, "/apps/{app}/releases/{id}") {
			shouldCapture = true
		}
	}

	// Only log when we decide to capture (to reduce noise)
	if shouldCapture {
		gtwlog.Infof(
			"shouldCaptureResponse: method=%s path=%s capture=%v process=%v",
			r.Method,
			path,
			shouldCapture,
			captureProcess,
		)
	}

	return shouldCapture, captureProcess
}

// writeResponse writes the response body to the writer.
func (h *Handler) writeResponse(
	w http.ResponseWriter,
	respReader io.Reader,
	body []byte,
	needsBuffer bool,
	shouldCaptureBody bool,
) (int64, []byte, error) {
	if needsBuffer {
		return h.writeBufferedResponse(w, respReader, body, shouldCaptureBody)
	}
	return h.writeStreamedResponse(w, respReader, shouldCaptureBody)
}

// isEnvironmentRead reports whether r reads an app's environment: a map of env var names to values.
func isEnvironmentRead(r *http.Request, path string) bool {
	return r.Method == http.MethodGet && rbac.KeyMatch3(path, "/apps/{app}/environment")
}

// logResponseHeaders logs the rack's response headers, with credentials redacted (see debugredact.Headers).
func (_ *Handler) logResponseHeaders(resp *http.Response) {
	if out := debugredact.Headers(resp.Header); out != "" {
		gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "upstream response headers\n%s", out)
	}
}

// urlForLog is the rack URL a request went to, as it may be logged: a rack URL can carry the rack's password.
func urlForLog(target *url.URL) string {
	return debugredact.URL(target.String())
}

// responseBodyForLog is the start of a rack response body as it may be logged: JSON with its secrets redacted
// (resource URLs carry credentials, and an app's environment is all values), or a placeholder, with the body's
// size, for one that can't be redacted.
func responseBodyForLog(r *http.Request, path string, snippet []byte, size int64) string {
	logged := debugredact.Body(snippet, proxyLogBodyLimit)
	if isEnvironmentRead(r, path) {
		logged = debugredact.EnvBody(snippet, proxyLogBodyLimit)
	}
	if logged == debugredact.TooLongPlaceholder || logged == debugredact.NotJSONPlaceholder {
		return fmt.Sprintf("%s (%d bytes)", logged, size)
	}
	return logged
}
