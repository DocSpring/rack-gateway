package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"

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
		filterEnvironment: isJSON && r.Method == http.MethodGet && rbac.KeyMatch3(path, "/apps/{app}/environment"),
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

// logResponseHeaders logs all response headers.
func (_ *Handler) logResponseHeaders(resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "%s: %s", key, value)
		}
	}
}
