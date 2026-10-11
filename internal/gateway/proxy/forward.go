package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const proxyLogBodyLimit = 16384

// attachedRunCommand is the placeholder command `convox run` starts before exec'ing the real command
// into the process (default --timeout). The real command is checked on the exec path.
const attachedRunCommand = "sleep 3600"

func (h *Handler) validateBuildRequest(
	r *http.Request,
	original string,
	bodyBytes []byte,
	authUser *auth.User,
) error {
	if r.Method != http.MethodPost || !rbac.KeyMatch3(original, "/apps/{app}/builds") {
		return nil
	}

	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		return fmt.Errorf("invalid build request body")
	}
	if err := refuseSurroundingWhitespace(vals, "url", "manifest"); err != nil {
		return err
	}

	if authUser.IsAPIToken {
		if tracker := getDeployApprovalTracker(r.Context()); tracker != nil && tracker.request != nil {
			return h.validateTokenBuildBinding(r, vals, tracker)
		}
	}
	return h.validateBuildManifestForAllUsers(r, vals)
}

// refuseSurroundingWhitespace refuses a build whose archive or manifest value starts or ends with whitespace. The
// rack uses these values exactly as sent, so "convox.yml " would be a different manifest (and a trailing space in
// the url a different object) from the one the gateway checked.
func refuseSurroundingWhitespace(vals url.Values, names ...string) error {
	for _, name := range names {
		if value := vals.Get(name); value != strings.TrimSpace(value) {
			return fmt.Errorf("invalid build request: %s must not start or end with whitespace", name)
		}
	}
	return nil
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
	if command != attachedRunCommand && !h.isCommandApproved(app, command) {
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
	filterEnvironment bool,
	shouldCapture bool,
	captureProcess bool,
) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if filterRelease {
		app := extractAppFromPath(pth)
		body = h.filterReleaseEnvForUser(r, body, app)
	}

	if filterEnvironment {
		app := extractAppFromPath(pth)
		body = h.filterEnvironmentMapResponse(r, body, app)
	}

	if shouldCapture {
		h.captureResourceCreator(r, pth, body, authUserEmail)
	}

	if captureProcess && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		h.handleProcessCreation(r, body)
	}

	return body, nil
}

func (h *Handler) handleProcessCreation(r *http.Request, body []byte) {
	tracker := getDeployApprovalTracker(r.Context())
	if tracker != nil {
		h.captureProcessCreation(r, body, tracker)
	}
}

// markDeployApprovalDeployedAfterPromote marks the approval behind a successful release promote as deployed,
// so the approval can't promote again. It runs for every proxied response: promote responses aren't buffered.
func (h *Handler) markDeployApprovalDeployedAfterPromote(r *http.Request, pth string, statusCode int) {
	if r.Method != http.MethodPost || statusCode < 200 || statusCode >= 300 ||
		!rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}/promote") {
		return
	}
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || h.database == nil {
		return
	}

	if err := h.database.MarkDeployApprovalAsDeployed(tracker.request.ID); err != nil {
		log.Printf("Failed to mark deploy approval as deployed: %v", err)
	}
}

func (_ *Handler) writeBufferedResponse(
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

func (_ *Handler) writeStreamedResponse(
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

func (_ *Handler) logProxyResponse(
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
	if !logProxy && !logResponse && (!logResponseBody || len(logSnippet) == 0) {
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

const (
	// defaultForwardTimeout bounds ordinary proxied rack API calls.
	defaultForwardTimeout = 30 * time.Second

	// promoteForwardTimeout gives release promotes room to finish. The Convox
	// API applies the entire release before responding (per-timer IAM/EKS
	// pod-identity roles, Kubernetes applies), which can legitimately take
	// minutes -- and canceling the request does NOT stop that work server-side.
	// Abandoned promotes keep consuming the rack API's Kubernetes client rate
	// limiter, so a short timeout plus retries makes every subsequent attempt
	// slower (observed: 30s timeouts stacked zombie promotes running 3-9
	// minutes each while deploys hard-failed).
	promoteForwardTimeout = 10 * time.Minute

	// uploadForwardTimeout gives an archive room to reach the rack and be processed: the gateway streams it from
	// the client as it arrives (up to uploadReadTimeout), and the rack answers once it has the whole archive and
	// has stored it, or for an import, pushed its images.
	uploadForwardTimeout = 20 * time.Minute
)

var promotePathPattern = regexp.MustCompile(`^/apps/[^/]+/releases/[^/]+/promote$`)

const (
	// objectUploadRoute is where `convox build` uploads the app's source archive.
	objectUploadRoute = "/apps/{app}/objects/tmp/{name}"
	// buildImportRoute is where `convox builds import` uploads an exported build.
	buildImportRoute = "/apps/{app}/builds/import"
)

// isObjectUpload reports whether r uploads a source archive to path.
func isObjectUpload(r *http.Request, path string) bool {
	return r.Method == http.MethodPost && rbac.KeyMatch3("/"+strings.TrimLeft(path, "/"), objectUploadRoute)
}

// isArchiveUpload reports whether r uploads an archive (a source archive or an exported build) to path.
func isArchiveUpload(r *http.Request, path string) bool {
	return isObjectUpload(r, path) ||
		(r.Method == http.MethodPost && rbac.KeyMatch3("/"+strings.TrimLeft(path, "/"), buildImportRoute))
}

// forwardTimeout selects the upstream timeout for a forwarded rack API request.
func forwardTimeout(r *http.Request, path string) time.Duration {
	normalized := "/" + strings.TrimLeft(path, "/")
	switch {
	case r.Method == http.MethodPost && promotePathPattern.MatchString(normalized):
		return promoteForwardTimeout
	case isArchiveUpload(r, normalized):
		return uploadForwardTimeout
	default:
		return defaultForwardTimeout
	}
}

func (h *Handler) forwardRequest(
	w http.ResponseWriter,
	r *http.Request,
	rack config.RackConfig,
	path string,
	authUser *auth.User,
) (int, error) {
	targetURL, err := buildTargetURL(rack, path, r.URL.RawQuery)
	if err != nil {
		return 0, err
	}

	if status := h.refuseUnforwardableOptions(w, r, authUser); status != 0 {
		return status, nil
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		return h.proxyWebSocket(w, r, rack, targetURL, authUser, path)
	}

	body, err := h.requestBodyForRack(r, path, authUser)
	if err != nil {
		return 0, err
	}

	if err := h.validateProcessCommand(r, path); err != nil {
		return 0, err
	}

	proxyReq, err := prepareProxyRequest(r, targetURL, body, rack, authUser)
	if err != nil {
		return 0, err
	}

	client, err := h.httpClient(r.Context(), forwardTimeout(r, path))
	if err != nil {
		log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
		return 0, fmt.Errorf("failed to prepare rack TLS: %w", err)
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			return 0, fpErr
		}
		return 0, fmt.Errorf("%w: %w", errRackRequestFailed, err)
	}
	defer resp.Body.Close() //nolint:errcheck,gosec // G104: cleanup

	return h.processProxyResponse(w, r, resp, path, authUser.Email)
}

// requestBodyForRack returns the body to send to the rack. An archive upload streams straight through as it
// arrives: the gateway doesn't inspect it (a source archive's manifest is checked when a build is created from it),
// and an archive can be far larger than the gateway should hold in memory. Every other body is read, within
// maxBufferedBodyBytes, so the gateway can check it.
func (h *Handler) requestBodyForRack(r *http.Request, path string, authUser *auth.User) (rackRequestBody, error) {
	if isArchiveUpload(r, path) {
		return rackRequestBody{reader: r.Body, length: r.ContentLength}, nil
	}

	bodyBytes, err := readBufferedBody(r)
	if err != nil {
		return rackRequestBody{}, err
	}

	if err := h.validateBuildRequest(r, path, bodyBytes, authUser); err != nil {
		return rackRequestBody{}, err
	}

	return rackRequestBody{reader: bytes.NewReader(bodyBytes), length: int64(len(bodyBytes))}, nil
}
