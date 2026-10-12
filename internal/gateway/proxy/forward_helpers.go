package proxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// maxBufferedBodyBytes caps a request body the gateway reads into memory, which it does for every rack API request
// except archive uploads (those stream to the rack). The largest real body is a release create carrying the app's
// whole env, which even with hundreds of variables comes to tens of KB. The gateway runs in a small container and
// reads several bodies at once, so the cap stays close to that: a few slow, oversized bodies must not exhaust it.
const maxBufferedBodyBytes = 1 << 20

// errBodyTooLarge refuses a request body over maxBufferedBodyBytes. The client gets a 413.
var errBodyTooLarge = fmt.Errorf(
	"request body too large: the gateway accepts at most %d MB", maxBufferedBodyBytes>>20,
)

// errBodyNotAllowed refuses a body on a GET, HEAD or DELETE request. The Convox SDK never sends one, so the gateway
// has nothing to forward and no reason to hold one in memory. Refusing it also keeps the rack from reading form
// values out of a multipart body (net/http parses one for any method) that the gateway never checked. The client
// gets a 400.
var errBodyNotAllowed = errors.New("request body not allowed: GET, HEAD and DELETE requests to the rack take no body")

// bodyErrorStatus is the HTTP status for a request the gateway couldn't handle: 413 for a body over
// maxBufferedBodyBytes, 400 for a body on a method that takes none, 502 for a rack response too large to filter,
// otherwise fallback.
func bodyErrorStatus(err error, fallback int) int {
	switch {
	case errors.Is(err, errBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, errBodyNotAllowed):
		return http.StatusBadRequest
	case errors.Is(err, errRackResponseTooLarge):
		return http.StatusBadGateway
	default:
		return fallback
	}
}

// rackRequestBody is the body the gateway sends to the rack, and its length (-1 when unknown).
type rackRequestBody struct {
	reader io.Reader
	length int64
}

// prepareProxyRequest creates a new HTTP request for proxying to the Convox rack.
func prepareProxyRequest(
	r *http.Request,
	targetURL string,
	body rackRequestBody,
	rack config.RackConfig,
	authUser *auth.User,
) (*http.Request, error) {
	// Validate targetURL is to configured rack to prevent SSRF
	if !strings.HasPrefix(targetURL, rack.URL) {
		return nil, fmt.Errorf("target URL does not match configured rack")
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, body.reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}
	// NewRequest only knows the length of an in-memory body. A streamed one keeps the client's length, so the
	// rack gets a Content-Length when the client sent one, and a chunked body otherwise.
	proxyReq.ContentLength = body.length

	copyForwardedHeaders(proxyReq.Header, r.Header)
	setGatewayHeaders(proxyReq.Header, rack, authUser)

	return proxyReq, nil
}

// readBufferedBody reads and closes the request body, returning the bytes. A body over maxBufferedBodyBytes is
// refused with errBodyTooLarge, and any body on a GET, HEAD or DELETE request with errBodyNotAllowed.
func readBufferedBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limit, refusal := int64(maxBufferedBodyBytes), errBodyTooLarge
	if !methodTakesBody(r.Method) {
		limit, refusal = 0, errBodyNotAllowed
	}
	if r.ContentLength > limit {
		return nil, refusal
	}

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, refusal
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if err := r.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close request body: %w", err)
	}

	return bodyBytes, nil
}

// methodTakesBody reports whether requests with this method may carry a body to the rack.
func methodTakesBody(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		return false
	default:
		return true
	}
}
