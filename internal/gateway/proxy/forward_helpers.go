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
// whole env: even hundreds of variables come to well under 1 MB, so 10 MB leaves plenty of room while keeping a
// large or malicious body from exhausting the gateway's memory.
const maxBufferedBodyBytes = 10 << 20

// errBodyTooLarge refuses a request body over maxBufferedBodyBytes. The client gets a 413.
var errBodyTooLarge = fmt.Errorf(
	"request body too large: the gateway accepts at most %d MB", maxBufferedBodyBytes>>20,
)

// bodyErrorStatus is the HTTP status for a request the gateway couldn't handle: 413 for a body over
// maxBufferedBodyBytes, otherwise fallback.
func bodyErrorStatus(err error, fallback int) int {
	if errors.Is(err, errBodyTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return fallback
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
// refused with errBodyTooLarge.
func readBufferedBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	if r.ContentLength > maxBufferedBodyBytes {
		return nil, errBodyTooLarge
	}

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBufferedBodyBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, errBodyTooLarge
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if err := r.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close request body: %w", err)
	}

	return bodyBytes, nil
}
