package proxy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const proxyLogBodyLimit = 16384

func (h *Handler) validateBuildRequest(
	r *http.Request,
	original string,
	bodyBytes []byte,
	authUser *auth.User,
) error {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(original, "/apps/{app}/builds") {
		return nil
	}

	if err := h.validateBuildManifestForAllUsers(r, bodyBytes); err != nil {
		return err
	}

	if !authUser.IsAPIToken {
		return nil
	}

	if authUser.TokenID == nil {
		return fmt.Errorf("API token authentication missing token ID")
	}

	return h.validateBuildRequestForAPIToken(r, bodyBytes, *authUser.TokenID)
}

func (h *Handler) validateProcessCommand(
	r *http.Request,
	original string,
) error {
	if r.Method != http.MethodPost ||
		!rbac.KeyMatch3(original, "/apps/{app}/services/{service}/processes") {
		return nil
	}

	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil {
		return nil
	}

	app := extractAppFromPath(original)
	command := strings.TrimSpace(r.Header.Get("Command"))
	if command != "sleep 3600" && !h.isCommandApproved(app, command) {
		return fmt.Errorf("command not approved: %s", command)
	}

	return nil
}

func (h *Handler) processBufferedResponse(
	r *http.Request,
	resp *http.Response,
	pth string,
	authUserEmail string,
	filterRelease bool,
	shouldCapture bool,
	captureProcess bool,
) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if filterRelease {
		body = h.filterReleaseEnvForUser(authUserEmail, body, false)
	}

	if shouldCapture {
		h.captureResourceCreator(r, pth, body, authUserEmail)
	}

	if captureProcess && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		h.handleProcessCreation(r, body)
	}

	if r.Method == http.MethodPost &&
		rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}/promote") &&
		resp.StatusCode >= 200 &&
		resp.StatusCode < 300 {
		h.markDeployApprovalDeployed(r)
	}

	return body, nil
}

func (h *Handler) handleProcessCreation(r *http.Request, body []byte) {
	tracker := getDeployApprovalTracker(r.Context())
	if tracker != nil {
		h.captureProcessCreation(r, body, tracker)
	}
}

func (h *Handler) markDeployApprovalDeployed(r *http.Request) {
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || h.database == nil {
		return
	}

	if err := h.database.MarkDeployApprovalAsDeployed(tracker.request.ID); err != nil {
		log.Printf("Failed to mark deploy approval as deployed: %v", err)
	}
}

func (h *Handler) writeBufferedResponse(
	w http.ResponseWriter,
	respReader io.Reader,
	body []byte,
	shouldCaptureBody bool,
) (int64, []byte, error) {
	bytesWritten, err := io.Copy(w, respReader)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to write response body: %w", err)
	}

	var logSnippet []byte
	if shouldCaptureBody {
		logSnippet = httputil.TruncateBytes(body, proxyLogBodyLimit)
	}

	return bytesWritten, logSnippet, nil
}

func (h *Handler) writeStreamedResponse(
	w http.ResponseWriter,
	respReader io.Reader,
	shouldCaptureBody bool,
) (int64, []byte, error) {
	if !shouldCaptureBody {
		bytesWritten, err := io.Copy(w, respReader)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to stream response body: %w", err)
		}
		return bytesWritten, nil, nil
	}

	acc := newLogAccumulator(proxyLogBodyLimit)
	reader := io.TeeReader(respReader, acc)
	bytesWritten, err := io.Copy(w, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to stream response body: %w", err)
	}

	return bytesWritten, acc.Bytes(), nil
}

func (h *Handler) logProxyResponse(
	r *http.Request,
	resp *http.Response,
	path string,
	contentType string,
	bytesWritten int64,
	logSnippet []byte,
	logProxy bool,
	logResponse bool,
	logResponseBody bool,
) {
	if !logProxy && !logResponse && !(logResponseBody && len(logSnippet) > 0) {
		return
	}

	upstreamMethod := ""
	upstreamURL := ""
	if resp.Request != nil {
		upstreamMethod = resp.Request.Method
		if resp.Request.URL != nil {
			upstreamURL = resp.Request.URL.String()
		}
	}

	if logProxy {
		gtwlog.DebugTopicf(
			gtwlog.TopicProxy,
			"upstream response %s %s -> %d ct=%q len=%d upstream_method=%s upstream_url=%q",
			r.Method,
			path,
			resp.StatusCode,
			contentType,
			bytesWritten,
			upstreamMethod,
			upstreamURL,
		)
	}

	if logResponse {
		gtwlog.DebugTopicf(
			gtwlog.TopicHTTPResponse,
			"upstream response %s %s -> %d ct=%q len=%d",
			r.Method,
			path,
			resp.StatusCode,
			contentType,
			bytesWritten,
		)
	}

	if logResponseBody && len(logSnippet) > 0 {
		gtwlog.DebugTopicf(
			gtwlog.TopicHTTPResponseBody,
			"upstream response %s %s -> %d body=%s",
			r.Method,
			path,
			resp.StatusCode,
			string(logSnippet),
		)
	}
}

func (h *Handler) forwardRequest(
	w http.ResponseWriter,
	r *http.Request,
	rack config.RackConfig,
	path string,
	authUser *auth.User,
) (int, error) {
	original := path
	base := strings.TrimRight(rack.URL, "/")
	p := "/" + strings.TrimLeft(path, "/")
	targetURL := base + p
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		return h.proxyWebSocket(w, r, rack, targetURL, authUser.Email, original)
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read request body: %w", err)
		}
		if err := r.Body.Close(); err != nil {
			return 0, fmt.Errorf("failed to close request body: %w", err)
		}
	}

	if err := h.validateBuildRequest(r, original, bodyBytes, authUser); err != nil {
		return 0, err
	}

	if err := h.validateProcessCommand(r, original); err != nil {
		return 0, err
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to create proxy request: %w", err)
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

	client, err := h.httpClient(r.Context(), 30*time.Second)
	if err != nil {
		log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
		return 0, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			return 0, fpErr
		}
		return 0, fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isJSON := strings.Contains(ct, "application/json")
	pth := original
	filterRelease := isJSON &&
		(rbac.KeyMatch3(pth, "/apps/{app}/releases") || rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}"))
	shouldCapture := false
	captureProcess := false
	if isJSON {
		switch r.Method {
		case http.MethodPost:
			shouldCapture = true
			if rbac.KeyMatch3(pth, "/apps/{app}/services/{service}/processes") {
				captureProcess = true
			}
		case http.MethodGet:
			if rbac.KeyMatch3(pth, "/apps/{app}/builds/{id}") || rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}") {
				shouldCapture = true
			}
		}
	}
	needsBuffer := filterRelease || shouldCapture || captureProcess

	logProxy := gtwlog.TopicEnabled(gtwlog.TopicProxy)
	logResponse := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponse)
	logResponseHeaders := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseHeaders)
	logResponseBody := gtwlog.TopicEnabled(gtwlog.TopicHTTPResponseBody)

	var body []byte
	var respReader io.Reader
	var bytesWritten int64
	var logSnippet []byte
	if needsBuffer {
		var err error
		body, err = h.processBufferedResponse(
			r,
			resp,
			pth,
			authUser.Email,
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

	if needsBuffer {
		bytesWritten, logSnippet, err = h.writeBufferedResponse(w, respReader, body, shouldCaptureBody)
	} else {
		bytesWritten, logSnippet, err = h.writeStreamedResponse(w, respReader, shouldCaptureBody)
	}
	if err != nil {
		return resp.StatusCode, err
	}

	if logResponseHeaders {
		for key, values := range resp.Header {
			for _, value := range values {
				gtwlog.DebugTopicf(gtwlog.TopicHTTPResponseHeaders, "%s: %s", key, value)
			}
		}
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
