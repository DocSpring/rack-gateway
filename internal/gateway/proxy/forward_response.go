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
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isJSON := strings.Contains(ct, "application/json")

	filterRelease := isJSON &&
		(rbac.KeyMatch3(path, "/apps/{app}/releases") || rbac.KeyMatch3(path, "/apps/{app}/releases/{id}"))

	shouldCapture, captureProcess := h.shouldCaptureResponse(r, path, isJSON)
	needsBuffer := filterRelease || shouldCapture || captureProcess

	logProxy := gtwlog.TopicEnabled(gtwlog.TopicProxy)
	logResponse := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponse)
	logResponseHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
	logResponseBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

	var body []byte
	var respReader io.Reader
	var err error

	if needsBuffer {
		body, err = h.processBufferedResponse(
			r,
			resp,
			path,
			authUserEmail,
			filterRelease,
			shouldCapture,
			captureProcess,
		)
		if err != nil {
			return 0, err
		}
		respReader = bytes.NewReader(body)
	} else {
		respReader = resp.Body
	}

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

// shouldCaptureResponse determines if the response should be captured.
func (h *Handler) shouldCaptureResponse(r *http.Request, path string, isJSON bool) (bool, bool) {
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
func (h *Handler) logResponseHeaders(resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "%s: %s", key, value)
		}
	}
}
