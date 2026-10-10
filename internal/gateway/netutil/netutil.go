package netutil

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type clientIPKey struct{}

// WithClientIP returns a copy of ctx carrying the request's client IP. The router's ClientIP middleware
// sets it once per request from gin's Context.ClientIP, which only believes X-Forwarded-For when the
// request arrives from a proxy in TRUSTED_PROXY_CIDRS.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// ClientIP returns the client IP resolved for r by the router, or the TCP peer address when r never passed
// through the router. It deliberately never reads forwarding headers itself: only the router knows which
// proxies to trust, and anyone can send an X-Forwarded-For header.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return peerIP(r)
}

// peerIP returns the IP address of the TCP peer that sent r (the last proxy, if there is one).
func peerIP(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
