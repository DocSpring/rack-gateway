package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
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

func (h *Handler) processBufferedResponse(
	r *http.Request,
	resp *http.Response,
	pth string,
	authUserEmail string,
	handling responseHandling,
) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBufferedResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > maxBufferedResponseBytes {
		return nil, errRackResponseTooLarge
	}

	if handling.filterRelease {
		app := extractAppFromPath(pth)
		body = h.filterReleaseEnvForUser(r, body, app)
	}

	if handling.filterEnvironment {
		app := extractAppFromPath(pth)
		body = h.filterEnvironmentMapResponse(r, body, app)
	}

	if handling.filterBuild {
		if body, err = stripBuildRepository(body); err != nil {
			return nil, fmt.Errorf("failed to filter build response: %w", err)
		}
	}

	if handling.capture {
		h.captureResourceCreator(r, pth, body, authUserEmail)
	}

	if handling.captureProcess && resp.StatusCode >= 200 && resp.StatusCode < 300 {
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

// markDeployApprovalDeployedAfterPromote marks the approval behind a successful release promote as deployed. It runs
// for every proxied response: promote responses aren't buffered. When that fails the approval still can't promote
// again: the promote keeps its claim, which never lapses.
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
		log.Printf("Failed to mark deploy approval %d as deployed (it keeps its promote claim): %v",
			tracker.request.ID, err)
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
			return bytesWritten, nil, fmt.Errorf("failed to stream response body: %w", err)
		}
		return bytesWritten, nil, nil
	}

	acc := newLogAccumulator(proxyLogBodyLimit)
	reader := io.TeeReader(respReader, acc)
	bytesWritten, err := io.Copy(w, reader)
	if err != nil {
		return bytesWritten, nil, fmt.Errorf("failed to stream response body: %w", err)
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
			upstreamURL = urlForLog(resp.Request.URL)
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
			responseBodyForLog(r, path, logSnippet, bytesWritten),
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
	// has stored it, or for an import, pushed its images. A build export streams the other way, and the client's
	// timeout covers reading the whole archive, so it gets the same.
	uploadForwardTimeout = 20 * time.Minute
)

// maxBufferedResponseBytes caps a rack response the gateway reads into memory to filter or record it. The largest
// is a release list at the most the gateway lets a client ask for (maxListLimit releases, each with the app's env
// and manifest), a few MB.
const maxBufferedResponseBytes = 16 << 20

// errRackResponseTooLarge refuses a rack response over maxBufferedResponseBytes. The client gets a 502.
var errRackResponseTooLarge = fmt.Errorf(
	"the rack's response is too large: the gateway reads at most %d MB of a response it filters",
	maxBufferedResponseBytes>>20,
)

// errResponseInterrupted marks a failure after the rack's response status was passed on, typically while its body
// streams: the client already has a status and part of the body, so the gateway can only stop. An error body would
// be appended to the response.
var errResponseInterrupted = errors.New("the response was interrupted")

var promotePathPattern = regexp.MustCompile(`^/apps/[^/]+/releases/[^/]+/promote$`)

const (
	// objectUploadRoute is where `convox build` uploads the app's source archive.
	objectUploadRoute = "/apps/{app}/objects/tmp/{name}"
	// buildImportRoute is where `convox builds import` uploads an exported build.
	buildImportRoute = "/apps/{app}/builds/import"
	// buildExportRoute is where `convox builds export` downloads a build's archive.
	buildExportRoute = "/apps/{app}/builds/{id}.tgz"
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
	case isArchiveUpload(r, normalized),
		r.Method == http.MethodGet && rbac.KeyMatch3(normalized, buildExportRoute):
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
	defer func() { _ = resp.Body.Close() }()

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

	if err := checkHeaderOnlyRequest(r, path, bodyBytes); err != nil {
		return rackRequestBody{}, err
	}
	if err := h.validateBuildRequest(r, path, bodyBytes, authUser); err != nil {
		return rackRequestBody{}, err
	}

	return rackRequestBody{reader: bytes.NewReader(bodyBytes), length: int64(len(bodyBytes))}, nil
}
