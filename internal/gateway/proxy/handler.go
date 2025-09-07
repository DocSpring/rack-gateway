package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/audit"
	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"net/url"
)

type Handler struct {
	config      *config.Config
	rbacManager rbac.RBACManager
	auditLogger *audit.Logger
}

func NewHandler(cfg *config.Config, rbacManager rbac.RBACManager, auditLogger *audit.Logger) *Handler {
	return &Handler{
		config:      cfg,
		rbacManager: rbacManager,
		auditLogger: auditLogger,
	}
}

// ProxyToRack handles all requests that should be proxied to the Convox rack
func (h *Handler) ProxyToRack(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Get the default rack (there's only one per gateway instance)
	rackConfig, exists := h.config.Racks["default"]
	if !exists {
		// Try local rack in dev mode
		rackConfig, exists = h.config.Racks["local"]
		if !exists {
			h.handleError(w, r, "no rack configured", http.StatusInternalServerError, "default", start)
			return
		}
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rackConfig.Name, start)
		return
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rackConfig.Name, start)
		return
	}

	// Get the full path including query params
	path := r.URL.Path

	// Check permissions (different logic for JWT vs API tokens)
	var allowed bool
	var err error
	methodForRBAC := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForRBAC = "SOCKET"
	}
	resource, action := h.pathToResourceAction(path, methodForRBAC)

	if authUser.IsAPIToken {
		// For API tokens, check permissions directly
		allowed = h.hasAPITokenPermission(authUser, resource, action)
	} else {
		// For JWT users, use RBAC
		allowed, err = h.rbacManager.Enforce(authUser.Email, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	// Forward the request to the rack
	status, err := h.forwardRequest(w, r, rackConfig, path, authUser.Email)
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rackConfig.Name, start)
		return
	}

	if status == 0 {
		status = http.StatusOK
	}
	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "allow", status, time.Since(start), nil)
}

func (h *Handler) ProxyRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	rack := chi.URLParam(r, "rack")
	path := chi.URLParam(r, "*")

	rackConfig, exists := h.config.Racks[rack]
	if !exists {
		h.handleError(w, r, "unknown rack", http.StatusNotFound, rack, start)
		return
	}

	if !rackConfig.Enabled {
		h.handleError(w, r, "rack disabled", http.StatusForbidden, rack, start)
		return
	}

	authUser, ok := auth.GetAuthUser(r.Context())
	if !ok {
		h.handleError(w, r, "unauthorized", http.StatusUnauthorized, rack, start)
		return
	}

	var allowed bool
	var err error
	methodForRBAC := r.Method
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		methodForRBAC = "SOCKET"
	}
	resource, action := h.pathToResourceAction(path, methodForRBAC)

	if authUser.IsAPIToken {
		// For API tokens, check permissions directly
		allowed = h.hasAPITokenPermission(authUser, resource, action)
	} else {
		// For JWT users, use RBAC
		allowed, err = h.rbacManager.Enforce(authUser.Email, resource, action)
		if err != nil {
			allowed = false
		}
	}

	if !allowed {
		h.auditLogger.LogRequest(r, authUser.Email, rack, "deny", http.StatusForbidden, time.Since(start), fmt.Errorf("permission denied for %s %s", r.Method, path))
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	status, err := h.forwardRequest(w, r, rackConfig, path, authUser.Email)
	if err != nil {
		h.handleError(w, r, err.Error(), http.StatusInternalServerError, rack, start)
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	h.auditLogger.LogRequest(r, authUser.Email, rack, "allow", status, time.Since(start), nil)
}

func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, rack config.RackConfig, path, userEmail string) (int, error) {
	// Build clean target URL without double slashes
	base := strings.TrimRight(rack.URL, "/")
	p := "/" + strings.TrimLeft(path, "/")
	targetURL := base + p
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Handle WebSocket upgrade requests
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
		return h.proxyWebSocket(w, r, rack, targetURL, userEmail)
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read request body: %w", err)
		}
		r.Body.Close()
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to create proxy request: %w", err)
	}

	for key, values := range r.Header {
		if key != "Authorization" {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}
	}

	// Convox uses Basic Auth with configurable username (default "convox") and the API key as password
	proxyReq.Header.Set("Authorization", fmt.Sprintf("Basic %s",
		base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	proxyReq.Header.Set("X-User-Email", userEmail)
	proxyReq.Header.Set("X-Request-ID", uuid.New().String())

	client := &http.Client{
		Timeout: 30 * time.Second,
		// Never follow redirects so we can observe upstream responses and preserve methods
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		return 0, fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close()

	// Read full response body (so we can optionally log it) then send to client
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	// Optional response logging
	if h.config.LogResponseBodies {
		max := h.config.LogResponseMaxBytes
		logBody := body
		if max > 0 && len(logBody) > max {
			logBody = append([]byte{}, logBody[:max]...)
			logBody = append(logBody, []byte("…(truncated)")...)
		}
		ct := resp.Header.Get("Content-Type")
		upstreamMethod := ""
		upstreamURL := ""
		if resp.Request != nil {
			upstreamMethod = resp.Request.Method
			if resp.Request.URL != nil {
				upstreamURL = resp.Request.URL.String()
			}
		}
		fmt.Printf("DEBUG RESPONSE %s %s -> %d ct=%q len=%d upstream_method=%s upstream_url=%q body=%s\n", r.Method, path, resp.StatusCode, ct, len(body), upstreamMethod, upstreamURL, string(logBody))
	}

	if _, err := w.Write(body); err != nil {
		return resp.StatusCode, fmt.Errorf("failed to write response body: %w", err)
	}

	return resp.StatusCode, nil
}

// proxyWebSocket upgrades the client connection and bridges it to the rack via a WebSocket connection
func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, rack config.RackConfig, target string, userEmail string) (int, error) {
	// Prepare upstream URL (ws or wss)
	u, err := url.Parse(target)
	if err != nil {
		return 0, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	// Dial upstream websocket with Authorization header
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	header.Set("X-User-Email", userEmail)
	header.Set("X-Request-ID", uuid.New().String())
	// Some servers validate Origin during WS handshake
	header.Set("Origin", fmt.Sprintf("%s://%s", map[bool]string{true: "https", false: "http"}[strings.HasPrefix(rack.URL, "https")], u.Host))
	// Forward relevant client headers to upstream (Convox uses headers for exec options, including 'command')
	for k, vals := range r.Header {
		lk := strings.ToLower(k)
		switch lk {
		case "authorization":
			// override with rack auth
			continue
		case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions":
			continue
		case "origin":
			// we already set Origin appropriate to upstream host
			continue
		case "sec-websocket-protocol":
			// handled below
			continue
		case "x-user-email", "x-request-id":
			// already set explicitly above; avoid duplicates
			continue
		}
		for _, v := range vals {
			header.Add(k, v)
		}
	}
	// Preserve subprotocols requested by client (needed for k8s exec multiplexing)
	if sp := r.Header.Get("Sec-WebSocket-Protocol"); sp != "" {
		header.Set("Sec-WebSocket-Protocol", sp)
	}
	d := *websocket.DefaultDialer
	d.HandshakeTimeout = 10 * time.Second

	// Follow up to 3 redirects for WS dial (some servers redirect mounting paths)
	var upstreamConn *websocket.Conn
	var resp *http.Response
	for i := 0; i < 3; i++ {
		upstreamConn, resp, err = d.Dial(u.String(), header)
		if err == nil {
			break
		}
		if resp != nil && (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect || resp.StatusCode == http.StatusSeeOther) {
			loc := resp.Header.Get("Location")
			if loc == "" {
				break
			}
			nu, perr := url.Parse(loc)
			if perr != nil {
				break
			}
			if !nu.IsAbs() {
				nu = u.ResolveReference(nu)
			}
			u = nu
			continue
		}
		break
	}
	if err != nil {
		if resp != nil {
			// If upstream returned a non-101 status (e.g., 404), pass it through to the client
			body, _ := io.ReadAll(resp.Body)
			for k, vs := range resp.Header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return resp.StatusCode, nil
		}
		return 0, fmt.Errorf("failed to dial upstream websocket: %w", err)
	}
	defer upstreamConn.Close()

	// Determine upstream-selected subprotocol (if any)
	selectedSP := ""
	if upstreamConn != nil {
		selectedSP = upstreamConn.Subprotocol()
	}

	// Upgrade client connection
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
		// Advertise only the upstream-selected subprotocol (pass-through) if present
		Subprotocols: func() []string {
			if selectedSP != "" {
				return []string{selectedSP}
			}
			return nil
		}(),
	}
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to upgrade client connection: %w", err)
	}
	defer clientConn.Close()

	// Bridge messages in both directions
	errc := make(chan error, 2)
	go func() {
		for {
			mt, message, err := clientConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := upstreamConn.WriteMessage(mt, message); err != nil {
				errc <- err
				return
			}
		}
	}()
	go func() {
		for {
			mt, message, err := upstreamConn.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if err := clientConn.WriteMessage(mt, message); err != nil {
				errc <- err
				return
			}
		}
	}()

	// Wait for either direction to error/close
	<-errc

	return http.StatusSwitchingProtocols, nil
}

func parseSubprotocols(h string) []string {
	if h == "" {
		return nil
	}
	parts := strings.Split(h, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pathToResourceAction converts a path and HTTP method to resource and action for RBAC
func (h *Handler) pathToResourceAction(path, method string) (string, string) {
	// Explicit mapping aligned to Convox routes
	type rule struct{ m, p, res, act string }
	rules := []rule{
		{"SOCKET", "/apps/{app}/processes/{pid}/exec", "ps", "manage"},
		{"DELETE", "/apps/{app}/processes/{pid}", "ps", "manage"},
		{"GET", "/apps/{app}/processes/{pid}", "ps", "list"},
		{"GET", "/apps/{app}/processes", "ps", "list"},
		{"POST", "/apps/{app}/services/{service}/processes", "ps", "manage"},

		{"SOCKET", "/apps/{app}/processes/{pid}/logs", "logs", "read"},
		{"SOCKET", "/apps/{app}/builds/{id}/logs", "logs", "read"},
		{"SOCKET", "/apps/{app}/logs", "logs", "read"},
		{"SOCKET", "/system/logs", "logs", "read"},

		{"GET", "/apps/{app}/builds", "builds", "list"},
		{"GET", "/apps/{app}/builds/{id}", "builds", "list"},
		{"GET", "/apps/{app}/builds/{id}.tgz", "builds", "list"},
		{"POST", "/apps/{app}/builds", "builds", "create"},
		{"POST", "/apps/{app}/builds/import", "builds", "create"},
		{"PUT", "/apps/{app}/builds/{id}", "builds", "create"},

		{"GET", "/apps/{app}/releases", "releases", "list"},
		{"GET", "/apps/{app}/releases/{id}", "releases", "list"},
		{"POST", "/apps/{app}/releases/{id}/promote", "releases", "promote"},

		{"GET", "/apps", "apps", "list"},
		{"GET", "/apps/{name}", "apps", "list"},
		{"POST", "/apps", "apps", "manage"},
		{"PUT", "/apps/{name}", "apps", "manage"},
		{"DELETE", "/apps/{name}", "apps", "manage"},

		{"GET", "/system", "rack", "read"},
		{"GET", "/system/capacity", "rack", "read"},
		{"GET", "/system/metrics", "rack", "read"},
		{"GET", "/system/processes", "rack", "read"},
		{"GET", "/system/releases", "rack", "read"},
	}

	for _, rl := range rules {
		if (rl.m == method || rl.m == "*") && keyMatch3(path, rl.p) {
			return rl.res, rl.act
		}
	}
	// Default conservative
	if method == "GET" {
		return "apps", "list"
	}
	return "apps", "manage"
}

// keyMatch3 simplified: supports {var} placeholders and wildcards
func keyMatch3(path, pattern string) bool {
	// Convert pattern to regex
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '{' {
			for i < len(pattern) && pattern[i] != '}' {
				i++
			}
			b.WriteString("[^/]+")
			continue
		}
		if c == '*' {
			b.WriteString(".*")
			continue
		}
		if strings.ContainsRune(".+?^$()[]{}|\\", rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteString("$")
	re := b.String()
	ok, _ := regexp.MatchString(re, path)
	return ok
}

// hasAPITokenPermission checks if an API token has the required permission
func (h *Handler) hasAPITokenPermission(authUser *auth.AuthUser, resource, action string) bool {
	permission := fmt.Sprintf("convox:%s:%s", resource, action)

	for _, perm := range authUser.Permissions {
		// Check for exact match
		if perm == permission {
			return true
		}
		// Check for wildcard matches
		if perm == "convox:*:*" || perm == fmt.Sprintf("convox:%s:*", resource) {
			return true
		}
	}

	return false
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, message string, status int, rack string, start time.Time) {
	userEmail := "anonymous"
	if authUser, ok := auth.GetAuthUser(r.Context()); ok {
		userEmail = authUser.Email
	}

	h.auditLogger.LogRequest(r, userEmail, rack, "error", status, time.Since(start), fmt.Errorf("%s", message))

	errorResponse := map[string]string{"error": message}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse)
}
