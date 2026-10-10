package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// loginTimeout matches the gateway's 10-minute limit for a CLI login.
const loginTimeout = 10 * time.Minute

var errLoginTimedOut = errors.New("login timed out waiting for browser authentication")

// Error codes the CLI itself ends a login with on the gateway's result page (the gateway's own codes
// are passed through). The page maps each code to a message.
const (
	loginErrorStateMismatch = "state_mismatch"
	loginErrorMissingCode   = "missing_code"
	loginErrorIncomplete    = "cli_incomplete"
)

// pkce holds an RFC 7636 code verifier and its S256 challenge.
type pkce struct {
	verifier  string
	challenge string
}

func newPKCE() (pkce, error) {
	verifier, err := randomURLSafe(64)
	if err != nil {
		return pkce{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return pkce{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}, nil
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

type loopbackResult struct {
	code string
	err  error
}

// loopbackServer receives the browser redirect that carries the single-use login code (RFC 8252).
// It only listens on 127.0.0.1 and only accepts the redirect carrying this login's state.
//
// The browser is held on the callback until the terminal has redeemed the login code and saved the
// session, then sent to the gateway's result page, so the browser shows how the login really ended.
type loopbackServer struct {
	redirectURI string
	state       string
	gatewayURL  string
	server      *http.Server
	results     chan loopbackResult

	finishOnce sync.Once
	finished   chan struct{}
	// outcome is the result page error code ("" for success); set before finished is closed.
	outcome string
}

func startLoopbackServer(state, gatewayURL string) (*loopbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen for the login redirect on 127.0.0.1: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("unexpected listener address %s", listener.Addr())
	}

	s := &loopbackServer{
		redirectURI: fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port),
		state:       state,
		gatewayURL:  gatewayURL,
		results:     make(chan loopbackResult, 1),
		finished:    make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

func (s *loopbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(s.state)) != 1 {
		// Not our login: ignore it and keep waiting for the real redirect.
		s.redirectToResult(w, r, loginErrorStateMismatch)
		return
	}
	if errCode := strings.TrimSpace(query.Get("error")); errCode != "" {
		s.deliver(loopbackResult{err: errors.New(loginErrorMessage(errCode))})
		s.redirectToResult(w, r, errCode)
		return
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		s.deliver(loopbackResult{err: errors.New("the gateway did not return a login code")})
		s.redirectToResult(w, r, loginErrorMissingCode)
		return
	}
	s.deliver(loopbackResult{code: code})
	select {
	case <-s.finished:
		s.redirectToResult(w, r, s.outcome)
	case <-r.Context().Done():
	}
}

func (s *loopbackServer) deliver(result loopbackResult) {
	select {
	case s.results <- result:
	default:
	}
}

// wait blocks until the browser delivers the login code, the login fails, or the timeout passes.
func (s *loopbackServer) wait(timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-s.results:
		return result.code, result.err
	case <-timer.C:
		return "", errLoginTimedOut
	}
}

// finish records how the login ended (errorCode "" for success) and releases the browser waiting on
// the callback. Only the first call counts.
func (s *loopbackServer) finish(errorCode string) {
	s.finishOnce.Do(func() {
		s.outcome = errorCode
		close(s.finished)
	})
}

func (s *loopbackServer) close() {
	// A browser still waiting here means the login ended without being finished.
	s.finish(loginErrorIncomplete)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// redirectToResult sends the browser to the gateway's CLI login result page (errorCode "" for success).
func (s *loopbackServer) redirectToResult(w http.ResponseWriter, r *http.Request, errorCode string) {
	target := buildGatewayAPIURL(s.gatewayURL, "/app/cli/auth/success")
	if errorCode != "" {
		query := url.Values{"error": {errorCode}}
		target = buildGatewayAPIURL(s.gatewayURL, "/app/cli/auth/error") + "?" + query.Encode()
	}
	// The callback URL carries the login code, so it must not leak as a referrer.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// loginErrorMessages maps the gateway's login error codes to messages for the terminal.
var loginErrorMessages = map[string]string{
	"unauthorized":            "your account is not authorized for this gateway",
	"exchange_failed":         "the identity provider login could not be completed",
	"session_incomplete":      "the login session was incomplete; run rack-gateway login again",
	"canceled":                "the login was canceled in the browser",
	"access_denied":           "the identity provider login was canceled or denied",
	"identity_provider_error": "the identity provider reported an error",
	"session_failed":          "the gateway could not start a browser session; run rack-gateway login again",
	"persist_failure":         "the gateway could not save the login; run rack-gateway login again",
	"load_failure":            "the gateway could not load the login; run rack-gateway login again",
}

func loginErrorMessage(code string) string {
	if message, ok := loginErrorMessages[code]; ok {
		return message
	}
	return fmt.Sprintf("the gateway could not complete the login (%s)", code)
}
