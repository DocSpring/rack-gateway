package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"
)

// loginTimeout matches the gateway's 10-minute limit for a CLI login.
const loginTimeout = 10 * time.Minute

var errLoginTimedOut = errors.New("login timed out waiting for browser authentication")

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
type loopbackServer struct {
	redirectURI string
	state       string
	gatewayURL  string
	server      *http.Server
	results     chan loopbackResult
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
		s.writePage(w, http.StatusBadRequest, "Login link mismatch",
			"This login does not match the rack-gateway login waiting in your terminal.")
		return
	}
	if errCode := strings.TrimSpace(query.Get("error")); errCode != "" {
		message := loginErrorMessage(errCode)
		s.writePage(w, http.StatusOK, "Login failed", message+". Return to your terminal.")
		s.deliver(loopbackResult{err: errors.New(message)})
		return
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		s.writePage(w, http.StatusBadRequest, "Login failed", "The login code is missing. Return to your terminal.")
		s.deliver(loopbackResult{err: errors.New("the gateway did not return a login code")})
		return
	}
	s.writePage(w, http.StatusOK, "Login approved", "You can close this tab and return to your terminal.")
	s.deliver(loopbackResult{code: code})
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

func (s *loopbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

var loopbackPage = template.Must(template.New("loopback").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
<p><a href="{{.WebURL}}">Open the Rack Gateway web UI</a></p>
</body>
</html>`))

func (s *loopbackServer) writePage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = loopbackPage.Execute(w, map[string]string{
		"Title":   title,
		"Message": message,
		"WebURL":  buildGatewayAPIURL(s.gatewayURL, "/app/"),
	})
}

// loginErrorMessage maps the gateway's login error codes to messages for the terminal.
func loginErrorMessage(code string) string {
	switch code {
	case "unauthorized":
		return "your account is not authorized for this gateway"
	case "exchange_failed":
		return "the identity provider login could not be completed"
	case "session_incomplete":
		return "the login session was incomplete; run rack-gateway login again"
	default:
		return fmt.Sprintf("the gateway could not complete the login (%s)", code)
	}
}
