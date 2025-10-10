package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routematch"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, rack config.RackConfig, path string, authUser *auth.AuthUser) (int, error) {
	original := path
	// Build clean target URL without double slashes
	base := strings.TrimRight(rack.URL, "/")
	p := "/" + strings.TrimLeft(path, "/")
	targetURL := base + p
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Handle WebSocket upgrade requests
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
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

	// Validate build manifests for ALL users (enforces image pattern security policy)
	if r.Method == http.MethodPost && routematch.KeyMatch3(original, "/apps/{app}/builds") {
		if err := h.validateBuildManifestForAllUsers(r, bodyBytes); err != nil {
			return 0, err
		}

		// Additionally validate deploy approval tracking for API tokens
		if authUser.IsAPIToken {
			if authUser.TokenID == nil {
				return 0, fmt.Errorf("API token authentication missing token ID")
			}
			if err := h.validateBuildRequestForAPIToken(r, bodyBytes, *authUser.TokenID); err != nil {
				return 0, err
			}
		}
	}

	// Validate process start commands for deploy approval flow
	if r.Method == http.MethodPost && routematch.KeyMatch3(original, "/apps/{app}/services/{service}/processes") {
		if tracker := getDeployApprovalTracker(r.Context()); tracker != nil {
			command := strings.TrimSpace(r.Header.Get("Command"))
			// Only allow hardcoded sleep command or approved commands
			if command != "sleep 3600" && !h.isCommandApproved(command) {
				return 0, fmt.Errorf("command not approved: %s", command)
			}
		}
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to create proxy request: %w", err)
	}

	for key, values := range r.Header {
		lk := strings.ToLower(key)
		if lk == "authorization" || lk == "env" || lk == "environment" || lk == "release-env" || lk == "x-audit-resource" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Convox uses Basic Auth with configurable username (default "convox") and the API key as password
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
	defer resp.Body.Close() //nolint:errcheck // response cleanup

	// Read full response body (so we can optionally log it and/or filter) then send to client
	// Decide whether we need to buffer the response (only for JSON we mutate or inspect)
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	isJSON := strings.Contains(ct, "application/json")
	pth := original
	filterRelease := isJSON && (routematch.KeyMatch3(pth, "/apps/{app}/releases") || routematch.KeyMatch3(pth, "/apps/{app}/releases/{id}"))
	shouldCapture := false
	captureProcess := false
	if isJSON {
		switch r.Method {
		case http.MethodPost:
			shouldCapture = true
			// Capture process creation for deploy approval tracking
			if routematch.KeyMatch3(pth, "/apps/{app}/services/{service}/processes") {
				captureProcess = true
			}
		case http.MethodGet:
			if routematch.KeyMatch3(pth, "/apps/{app}/builds/{id}") || routematch.KeyMatch3(pth, "/apps/{app}/releases/{id}") {
				shouldCapture = true
			}
		}
	}
	needsBuffer := filterRelease || shouldCapture || captureProcess

	var body []byte
	var respReader io.Reader
	var bytesWritten int64
	var logSnippet []byte
	if needsBuffer {
		var err error
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read response body: %w", err)
		}
		if filterRelease {
			body = h.filterReleaseEnvForUser(authUser.Email, body, false)
		}
		if shouldCapture {
			h.captureResourceCreator(r, pth, body, authUser.Email)
		}
		// Only track processes created via deploy approval flow
		if captureProcess && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if tracker := getDeployApprovalTracker(r.Context()); tracker != nil {
				h.captureProcessCreation(r, body, tracker)
			}
		}
		// Mark deploy approval as deployed after successful release promotion
		if r.Method == http.MethodPost && routematch.KeyMatch3(pth, "/apps/{app}/releases/{id}/promote") && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if tracker := getDeployApprovalTracker(r.Context()); tracker != nil {
				if h.database != nil {
					if err := h.database.MarkDeployApprovalAsDeployed(tracker.request.ID); err != nil {
						log.Printf("Failed to mark deploy approval as deployed: %v", err)
					}
				}
			}
		}
		respReader = bytes.NewReader(body)
	} else {
		respReader = resp.Body
	}

	// Copy headers, but drop Content-Length since we may have modified the body; let Go recalculate
	for key, values := range resp.Header {
		if strings.ToLower(key) == "content-length" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if needsBuffer {
		var err error
		bytesWritten, err = io.Copy(w, respReader)
		if err != nil {
			return resp.StatusCode, fmt.Errorf("failed to write response body: %w", err)
		}
		if h.config.LogResponseBodies {
			max := h.config.LogBodyMaxBytes
			logBody := body
			if max > 0 && len(logBody) > max {
				logBody = append([]byte{}, logBody[:max]...)
				logBody = append(logBody, []byte("…(truncated)")...)
			}
			logSnippet = logBody
		}
	} else {
		if h.config.LogResponseBodies {
			acc := newLogAccumulator(h.config.LogBodyMaxBytes)
			reader := io.TeeReader(respReader, acc)
			var err error
			bytesWritten, err = io.Copy(w, reader)
			if err != nil {
				return resp.StatusCode, fmt.Errorf("failed to stream response body: %w", err)
			}
			logSnippet = acc.Bytes()
		} else {
			var err error
			bytesWritten, err = io.Copy(w, respReader)
			if err != nil {
				return resp.StatusCode, fmt.Errorf("failed to stream response body: %w", err)
			}
		}
	}

	// Optional response logging
	if h.config.LogResponseBodies {
		ctHeader := resp.Header.Get("Content-Type")
		upstreamMethod := ""
		upstreamURL := ""
		if resp.Request != nil {
			upstreamMethod = resp.Request.Method
			if resp.Request.URL != nil {
				upstreamURL = resp.Request.URL.String()
			}
		}
		fmt.Printf("DEBUG RESPONSE %s %s -> %d ct=%q len=%d upstream_method=%s upstream_url=%q body=%s\n",
			r.Method, path, resp.StatusCode, ctHeader, bytesWritten, upstreamMethod, upstreamURL, string(logSnippet))
	}

	return resp.StatusCode, nil
}

func (h *Handler) filterReleaseEnvForUser(email string, body []byte, _ bool) []byte {
	// Determine permissions
	canEnvView, _ := h.rbacManager.Enforce(email, rbac.ScopeConvox, rbac.ResourceEnv, rbac.ActionRead)
	// Note: For native release responses, ALWAYS mask secrets regardless of secrets:read.

	// If no environment view, mask all env values (do not strip, to avoid accidental clears)
	if !canEnvView {
		var any interface{}
		if err := json.Unmarshal(body, &any); err != nil {
			return body
		}
		maskAll := func(s string) string {
			lines := strings.Split(s, "\n")
			for i, ln := range lines {
				if ln == "" {
					continue
				}
				parts := strings.SplitN(ln, "=", 2)
				if len(parts) == 2 {
					parts[1] = maskedSecret
					lines[i] = parts[0] + "=" + parts[1]
				}
			}
			return strings.Join(lines, "\n")
		}
		switch v := any.(type) {
		case map[string]interface{}:
			if envv, ok := v["env"].(string); ok {
				v["env"] = maskAll(envv)
			}
			nb, _ := json.Marshal(v)
			return nb
		case []interface{}:
			for _, it := range v {
				if m, ok := it.(map[string]interface{}); ok {
					if envv, ok2 := m["env"].(string); ok2 {
						m["env"] = maskAll(envv)
					}
				}
			}
			nb, _ := json.Marshal(v)
			return nb
		default:
			return body
		}
	}

	// Env read allowed; redact secrets (always, regardless of secrets:read)
	var any interface{}
	if err := json.Unmarshal(body, &any); err != nil {
		return body
	}
	mask := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if ln == "" {
				continue
			}
			parts := strings.SplitN(ln, "=", 2)
			key := parts[0]
			if h.isSecretKey(key) && len(parts) > 1 {
				parts[1] = maskedSecret
				lines[i] = parts[0] + "=" + parts[1]
			}
		}
		return strings.Join(lines, "\n")
	}
	switch v := any.(type) {
	case map[string]interface{}:
		if envv, ok := v["env"].(string); ok {
			v["env"] = mask(envv)
		}
		nb, _ := json.Marshal(v)
		return nb
	case []interface{}:
		for _, it := range v {
			if m, ok := it.(map[string]interface{}); ok {
				if envv, ok2 := m["env"].(string); ok2 {
					m["env"] = mask(envv)
				}
			}
		}
		nb, _ := json.Marshal(v)
		return nb
	default:
		return body
	}
}

func (h *Handler) isSecretKey(key string) bool {
	// Merge configured secret names and protected names (always masked)
	extra := make([]string, 0, len(h.secretNames)+len(h.protectedEnv))
	for k := range h.secretNames {
		extra = append(extra, k)
	}
	for k := range h.protectedEnv {
		extra = append(extra, k)
	}
	return envutil.IsSecretKey(key, extra)
}

func (h *Handler) isProtectedKey(key string) bool {
	_, ok := h.protectedEnv[strings.ToUpper(strings.TrimSpace(key))]
	return ok
}

func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, rack config.RackConfig, target string, userEmail string, originalPath string) (int, error) {
	// Validate and track exec commands for deploy approval flow
	if routematch.KeyMatch3(originalPath, "/apps/{app}/processes/{id}/exec") {
		if tracker := getDeployApprovalTracker(r.Context()); tracker != nil {
			processID := extractProcessIDFromPath(originalPath)
			command := strings.TrimSpace(r.Header.Get("Command"))
			if processID != "" && command != "" && h.database != nil {
				// Validate command is in approved list
				if !h.isCommandApproved(command) {
					http.Error(w, forbiddenMessage(rbac.ResourceProcess, rbac.ActionExec), http.StatusForbidden)
					return http.StatusForbidden, nil
				}

				// Track executed command for auditing
				if err := h.database.AppendExecCommandToDeployApprovalRequest(tracker.request.ID, processID, command); err != nil {
					log.Printf("Failed to track exec command in deploy approval: %v", err)
				}
			}
		}
	}

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
		case "x-audit-resource":
			// internal auditing header; never forward upstream
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
	if strings.HasPrefix(strings.ToLower(rack.URL), "https://") {
		cfg, err := h.rackTLSConfig(r.Context())
		if err != nil {
			return 0, fmt.Errorf("failed to prepare rack TLS: %w", err)
		}
		if cfg != nil {
			d.TLSClientConfig = cfg
		} else {
			d.TLSClientConfig = httpclient.NewRackTLSConfig()
		}
	}

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
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("websocket_dial", fpErr)
			return 0, fpErr
		}
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
	defer upstreamConn.Close() //nolint:errcheck // websocket cleanup

	// Determine upstream-selected subprotocol (if any)
	selectedSP := ""
	if upstreamConn != nil {
		selectedSP = upstreamConn.Subprotocol()
	}

	// Upgrade client connection
	upgrader := websocket.Upgrader{
		CheckOrigin: h.checkWebSocketOrigin,
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
	defer clientConn.Close() //nolint:errcheck // websocket cleanup

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

func (h *Handler) checkWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No origin header - allow for non-browser clients (CLI tools)
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		// Invalid origin URL - reject
		return false
	}

	// Allow same-origin requests
	if r.Host == originURL.Host {
		return true
	}

	// In development mode, be more permissive
	if os.Getenv("DEV_MODE") == "true" {
		// Allow localhost origins in dev
		if strings.HasPrefix(originURL.Host, "localhost:") || originURL.Host == "localhost" {
			return true
		}
		// Allow the configured web dev server
		if webDevURL := os.Getenv("WEB_DEV_SERVER_URL"); webDevURL != "" {
			if devURL, err := url.Parse(webDevURL); err == nil {
				if originURL.Host == devURL.Host {
					return true
				}
			}
		}
	}

	// Allow configured domain
	if h.config.Domain != "" {
		// Check if origin matches the configured domain
		allowedHost := h.config.Domain
		if !strings.Contains(allowedHost, ":") && originURL.Scheme == "https" {
			allowedHost = h.config.Domain + ":443"
		} else if !strings.Contains(allowedHost, ":") && originURL.Scheme == "http" {
			allowedHost = h.config.Domain + ":80"
		}

		// Compare without default ports
		originHost := originURL.Host
		if (originURL.Scheme == "https" && strings.HasSuffix(originHost, ":443")) ||
			(originURL.Scheme == "http" && strings.HasSuffix(originHost, ":80")) {
			originHost = strings.Split(originHost, ":")[0]
		}
		if (h.config.Domain == originHost) || (allowedHost == originURL.Host) {
			return true
		}
	}

	// Reject all other origins
	return false
}

// captureProcessCreation extracts process ID from the response and tracks it in the deploy approval request
func (h *Handler) captureProcessCreation(r *http.Request, body []byte, tracker *deployApprovalTracker) {
	if h.database == nil || tracker == nil {
		return
	}

	// Parse response to extract process ID
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("Failed to parse process creation response: %v", err)
		return
	}

	processID, ok := resp["id"].(string)
	if !ok || processID == "" {
		log.Printf("Process ID not found in response")
		return
	}

	// Track process ID only (not the sleep command - that's just a placeholder)
	// Actual exec commands will be tracked when process:exec happens
	if err := h.database.AppendProcessIDToDeployApprovalRequest(tracker.request.ID, processID); err != nil {
		log.Printf("Failed to track process ID in deploy approval: %v", err)
	}
}

// isCommandApproved checks if a command is in the approved commands list
func (h *Handler) isCommandApproved(command string) bool {
	if h.database == nil {
		return false
	}

	approvedCommands, err := h.database.GetApprovedCommands()
	if err != nil {
		log.Printf("Failed to get approved commands: %v", err)
		return false
	}

	// If no approved commands configured, allow all commands
	if len(approvedCommands) == 0 {
		return true
	}

	// Check if command matches any approved command
	for _, approved := range approvedCommands {
		if strings.TrimSpace(command) == strings.TrimSpace(approved) {
			return true
		}
	}

	return false
}
