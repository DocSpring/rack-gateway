package proxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/httpclient"
	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, rack config.RackConfig, target string, userEmail string, originalPath string) (int, error) {
	if rbac.KeyMatch3(originalPath, "/apps/{app}/processes/{id}/exec") {
		if tracker := getDeployApprovalTracker(r.Context()); tracker != nil {
			app := extractAppFromPath(originalPath)
			processID := extractProcessIDFromPath(originalPath)
			command := strings.TrimSpace(r.Header.Get("Command"))
			if processID != "" && command != "" && h.database != nil {
				if !h.isCommandApproved(app, command) {
					http.Error(w, forbiddenMessage(rbac.ResourceProcess, rbac.ActionExec), http.StatusForbidden)
					return http.StatusForbidden, nil
				}

				if err := h.database.AppendExecCommandToDeployApprovalRequest(tracker.request.ID, processID, command); err != nil {
					log.Printf("Failed to track exec command in deploy approval: %v", err)
				}
			}
		}
	}

	u, err := url.Parse(target)
	if err != nil {
		return 0, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
	header.Set("X-User-Email", userEmail)
	header.Set("X-Request-ID", uuid.New().String())
	header.Set("Origin", fmt.Sprintf("%s://%s", map[bool]string{true: "https", false: "http"}[strings.HasPrefix(rack.URL, "https")], u.Host))
	for k, vals := range r.Header {
		lk := strings.ToLower(k)
		switch lk {
		case "authorization", "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "origin", "sec-websocket-protocol", "x-user-email", "x-request-id", "x-audit-resource":
			continue
		}
		for _, v := range vals {
			header.Add(k, v)
		}
	}
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
			body, _ := io.ReadAll(resp.Body)
			httputil.CopyHeaders(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return resp.StatusCode, nil
		}
		return 0, fmt.Errorf("failed to dial upstream websocket: %w", err)
	}
	defer upstreamConn.Close() //nolint:errcheck

	selectedSP := ""
	if upstreamConn != nil {
		selectedSP = upstreamConn.Subprotocol()
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: h.checkWebSocketOrigin,
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
	defer clientConn.Close() //nolint:errcheck

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

	<-errc

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

	if os.Getenv("DEV_MODE") == "true" {
		if strings.HasPrefix(originURL.Host, "localhost:") || originURL.Host == "localhost" {
			return true
		}
		if webDevURL := os.Getenv("WEB_DEV_SERVER_URL"); webDevURL != "" {
			if devURL, err := url.Parse(webDevURL); err == nil {
				if originURL.Host == devURL.Host {
					return true
				}
			}
		}
	}

	if h.config.Domain != "" {
		allowedHost := h.config.Domain
		if !strings.Contains(allowedHost, ":") && originURL.Scheme == "https" {
			allowedHost = h.config.Domain + ":443"
		} else if !strings.Contains(allowedHost, ":") && originURL.Scheme == "http" {
			allowedHost = h.config.Domain + ":80"
		}

		originHost := originURL.Host
		if (originURL.Scheme == "https" && strings.HasSuffix(originHost, ":443")) ||
			(originURL.Scheme == "http" && strings.HasSuffix(originHost, ":80")) {
			originHost = strings.Split(originHost, ":")[0]
		}
		if h.config.Domain == originHost || allowedHost == originURL.Host {
			return true
		}
	}

	return false
}
