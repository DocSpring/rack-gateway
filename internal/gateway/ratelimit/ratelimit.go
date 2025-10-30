package ratelimit

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter manages per-IP rate limiting for authentication endpoints
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     rate.Limit // requests per second
	burst    int        // max burst size
	cleanup  time.Duration
	stop     chan struct{} // channel to stop cleanup goroutine
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter
// Default: 10 requests per second with burst of 20 (very generous for auth endpoints)
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 10
	}
	if burst <= 0 {
		burst = 20
	}

	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(requestsPerSecond),
		burst:    burst,
		cleanup:  5 * time.Minute,
		stop:     make(chan struct{}),
	}

	// Start cleanup goroutine to remove old visitors
	go rl.cleanupVisitors()

	return rl
}

// getVisitor retrieves or creates a rate limiter for the given IP
func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.visitors[ip] = &visitor{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupVisitors removes old entries from the visitors map
func (rl *RateLimiter) cleanupVisitors() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastSeen) > rl.cleanup {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (common behind proxies/load balancers)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP if there are multiple
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	// RemoteAddr might be in format "IP:port" so extract just the IP
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}

	return r.RemoteAddr
}

// Middleware returns an HTTP middleware that enforces rate limiting
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		limiter := rl.getVisitor(ip)
		if !limiter.Allow() {
			// Return 429 Too Many Requests
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(1/float64(rl.rate))))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", int(rl.rate)))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))

			w.WriteHeader(http.StatusTooManyRequests)
			errorMsg := `{"error":"rate limit exceeded",` +
				`"message":"too many authentication attempts, please try again later"}`
			if _, err := w.Write([]byte(errorMsg)); err != nil {
				return
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthEndpointsOnly wraps the middleware to only apply to auth-related endpoints
func (rl *RateLimiter) AuthEndpointsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only rate limit auth-related endpoints
		path := r.URL.Path
		if strings.Contains(path, "/auth/") ||
			strings.Contains(path, "/login") ||
			strings.Contains(path, "/logout") ||
			strings.Contains(path, "/callback") ||
			strings.Contains(path, "/tokens") && r.Method == "POST" {
			// Apply rate limiting
			ip := getClientIP(r)
			limiter := rl.getVisitor(ip)
			if !limiter.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(1/float64(rl.rate))))
				w.WriteHeader(http.StatusTooManyRequests)
				errorMsg := `{"error":"rate limit exceeded",` +
					`"message":"too many authentication attempts, please try again later"}`
				if _, err := w.Write([]byte(errorMsg)); err != nil {
					return
				}
				return
			}
		}

		// Pass through for non-auth endpoints
		next.ServeHTTP(w, r)
	})
}
