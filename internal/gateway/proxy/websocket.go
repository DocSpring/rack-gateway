package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *Handler) proxyWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	rack config.RackConfig,
	target string,
	userEmail string,
	originalPath string,
) (int, error) {
	if status := h.validateExecCommand(w, r, originalPath); status != 0 {
		return status, nil
	}

	wsURL, err := h.prepareWebSocketURL(target)
	if err != nil {
		return 0, err
	}

	header := h.buildWebSocketHeaders(r, rack, userEmail, wsURL)

	upstreamConn, resp, err := h.dialUpstreamWebSocket(r.Context(), wsURL, header, rack.URL)
	if err != nil {
		return h.handleDialError(w, resp, err)
	}
	defer upstreamConn.Close() //nolint:errcheck

	clientConn, err := h.upgradeClientConnection(w, r, upstreamConn)
	if err != nil {
		return 0, fmt.Errorf("failed to upgrade client connection: %w", err)
	}
	defer clientConn.Close() //nolint:errcheck

	h.proxyWebSocketMessages(clientConn, upstreamConn)
	return http.StatusSwitchingProtocols, nil
}

func (h *Handler) checkWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if r.Host == originURL.Host {
		return true
	}

	if h.isDevModeOriginAllowed(originURL) {
		return true
	}

	return h.isConfiguredDomainMatch(originURL)
}

// isDevModeOriginAllowed checks if origin is allowed in development mode
func (h *Handler) isDevModeOriginAllowed(originURL *url.URL) bool {
	if os.Getenv("DEV_MODE") != "true" {
		return false
	}

	if h.isLocalhostOrigin(originURL.Host) {
		return true
	}

	return h.isWebDevServerOrigin(originURL.Host)
}

// isLocalhostOrigin checks if host is localhost
func (h *Handler) isLocalhostOrigin(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "localhost:")
}

// isWebDevServerOrigin checks if host matches WEB_DEV_SERVER_URL
func (h *Handler) isWebDevServerOrigin(host string) bool {
	webDevURL := os.Getenv("WEB_DEV_SERVER_URL")
	if webDevURL == "" {
		return false
	}

	devURL, err := url.Parse(webDevURL)
	if err != nil {
		return false
	}

	return host == devURL.Host
}

// isConfiguredDomainMatch checks if origin matches configured domain
func (h *Handler) isConfiguredDomainMatch(originURL *url.URL) bool {
	if h.config.Domain == "" {
		return false
	}

	allowedHost := h.buildAllowedHost(originURL.Scheme)
	normalizedOrigin := h.normalizeOriginHost(originURL)

	return h.config.Domain == normalizedOrigin || allowedHost == originURL.Host
}

// buildAllowedHost adds default port to domain if needed
func (h *Handler) buildAllowedHost(scheme string) string {
	if strings.Contains(h.config.Domain, ":") {
		return h.config.Domain
	}

	if scheme == "https" {
		return h.config.Domain + ":443"
	}
	if scheme == "http" {
		return h.config.Domain + ":80"
	}
	return h.config.Domain
}

// normalizeOriginHost removes default ports from origin host
func (h *Handler) normalizeOriginHost(originURL *url.URL) string {
	host := originURL.Host

	if h.hasDefaultPort(originURL.Scheme, host) {
		return strings.Split(host, ":")[0]
	}

	return host
}

// hasDefaultPort checks if host has default port for scheme
func (h *Handler) hasDefaultPort(scheme, host string) bool {
	return (scheme == "https" && strings.HasSuffix(host, ":443")) ||
		(scheme == "http" && strings.HasSuffix(host, ":80"))
}

// prepareWebSocketURL converts HTTP(S) target to WS(S) URL
func (h *Handler) prepareWebSocketURL(target string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	return u, nil
}

// buildWebSocketHeaders constructs headers for upstream WebSocket connection
func (h *Handler) buildWebSocketHeaders(
	r *http.Request,
	rack config.RackConfig,
	userEmail string,
	wsURL *url.URL,
) http.Header {
	header := http.Header{}
	authValue := fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(authValue)))
	header.Set("X-User-Email", userEmail)
	header.Set("X-Request-ID", uuid.New().String())

	scheme := "http"
	if strings.HasPrefix(rack.URL, "https") {
		scheme = "https"
	}
	header.Set("Origin", fmt.Sprintf("%s://%s", scheme, wsURL.Host))

	h.copyClientHeaders(r.Header, header)

	if sp := r.Header.Get("Sec-WebSocket-Protocol"); sp != "" {
		header.Set("Sec-WebSocket-Protocol", sp)
	}
	return header
}

// copyClientHeaders copies allowed headers from client request
func (h *Handler) copyClientHeaders(src, dst http.Header) {
	excludedHeaders := map[string]bool{
		"authorization":            true,
		"host":                     true,
		"connection":               true,
		"upgrade":                  true,
		"sec-websocket-key":        true,
		"sec-websocket-version":    true,
		"sec-websocket-extensions": true,
		"origin":                   true,
		"sec-websocket-protocol":   true,
		"x-user-email":             true,
		"x-request-id":             true,
		"x-audit-resource":         true,
	}

	for k, vals := range src {
		if excludedHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// dialUpstreamWebSocket establishes connection to upstream WebSocket
func (h *Handler) dialUpstreamWebSocket(
	ctx context.Context,
	wsURL *url.URL,
	header http.Header,
	rackURL string,
) (*websocket.Conn, *http.Response, error) {
	d := *websocket.DefaultDialer
	d.HandshakeTimeout = 10 * time.Second

	if strings.HasPrefix(strings.ToLower(rackURL), "https://") {
		tlsCfg, err := h.rackTLSConfig(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to prepare rack TLS: %w", err)
		}
		if tlsCfg != nil {
			d.TLSClientConfig = tlsCfg
		} else {
			d.TLSClientConfig = httpclient.NewRackTLSConfig()
		}
	}

	return h.dialWithRedirects(&d, wsURL, header)
}

// dialWithRedirects attempts to dial with up to 3 redirect attempts
func (h *Handler) dialWithRedirects(
	dialer *websocket.Dialer,
	wsURL *url.URL,
	header http.Header,
) (*websocket.Conn, *http.Response, error) {
	var conn *websocket.Conn
	var resp *http.Response
	var err error

	for i := 0; i < 3; i++ {
		conn, resp, err = dialer.Dial(wsURL.String(), header)
		if err == nil {
			break
		}

		if !h.isRedirectResponse(resp) {
			break
		}

		newURL, parseErr := h.parseRedirectLocation(resp, wsURL)
		if parseErr != nil {
			break
		}
		wsURL = newURL
	}

	return conn, resp, err
}

// isRedirectResponse checks if response is a redirect
func (h *Handler) isRedirectResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return resp.StatusCode == http.StatusMovedPermanently ||
		resp.StatusCode == http.StatusFound ||
		resp.StatusCode == http.StatusTemporaryRedirect ||
		resp.StatusCode == http.StatusPermanentRedirect ||
		resp.StatusCode == http.StatusSeeOther
}

// parseRedirectLocation extracts and resolves redirect URL
func (h *Handler) parseRedirectLocation(resp *http.Response, base *url.URL) (*url.URL, error) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil, fmt.Errorf("empty redirect location")
	}

	newURL, err := url.Parse(loc)
	if err != nil {
		return nil, err
	}

	if !newURL.IsAbs() {
		newURL = base.ResolveReference(newURL)
	}
	return newURL, nil
}

// handleDialError processes errors from upstream dial attempts
func (h *Handler) handleDialError(
	w http.ResponseWriter,
	resp *http.Response,
	err error,
) (int, error) {
	if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
		logRackTLSMismatch("websocket_dial", fpErr)
		return 0, fpErr
	}

	if resp != nil {
		body, _ := io.ReadAll(resp.Body)
		httputil.CopyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return resp.StatusCode, nil
	}

	return 0, fmt.Errorf("failed to dial upstream websocket: %w", err)
}

// upgradeClientConnection upgrades client HTTP connection to WebSocket
func (h *Handler) upgradeClientConnection(
	w http.ResponseWriter,
	r *http.Request,
	upstreamConn *websocket.Conn,
) (*websocket.Conn, error) {
	var subprotocols []string
	if selectedSP := upstreamConn.Subprotocol(); selectedSP != "" {
		subprotocols = []string{selectedSP}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin:  h.checkWebSocketOrigin,
		Subprotocols: subprotocols,
	}

	return upgrader.Upgrade(w, r, nil)
}

// proxyWebSocketMessages bidirectionally proxies WebSocket messages
func (h *Handler) proxyWebSocketMessages(clientConn, upstreamConn *websocket.Conn) {
	errc := make(chan error, 2)

	go h.forwardMessages(clientConn, upstreamConn, errc)
	go h.forwardMessages(upstreamConn, clientConn, errc)

	<-errc
}

// forwardMessages forwards messages from source to destination
func (h *Handler) forwardMessages(src, dst *websocket.Conn, errc chan error) {
	for {
		msgType, message, err := src.ReadMessage()
		if err != nil {
			errc <- err
			return
		}
		if err := dst.WriteMessage(msgType, message); err != nil {
			errc <- err
			return
		}
	}
}

// validateExecCommand validates exec commands for deploy approval tracking
// Returns HTTP status code if request should be denied, 0 otherwise
func (h *Handler) validateExecCommand(w http.ResponseWriter, r *http.Request, originalPath string) int {
	if !rbac.KeyMatch3(originalPath, "/apps/{app}/processes/{id}/exec") {
		return 0
	}

	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil {
		return 0
	}

	app := extractAppFromPath(originalPath)
	processID := extractProcessIDFromPath(originalPath)
	command := strings.TrimSpace(r.Header.Get("Command"))

	if processID == "" || command == "" || h.database == nil {
		return 0
	}

	if !h.isCommandApproved(app, command) {
		http.Error(w, forbiddenMessage(rbac.ResourceProcess, rbac.ActionExec), http.StatusForbidden)
		return http.StatusForbidden
	}

	err := h.database.AppendExecCommandToDeployApprovalRequest(tracker.request.ID, processID, command)
	if err != nil {
		log.Printf("Failed to track exec command in deploy approval: %v", err)
	}

	return 0
}
