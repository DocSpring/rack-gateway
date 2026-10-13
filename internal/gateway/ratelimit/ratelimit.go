package ratelimit

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// RateLimiter manages per-IP rate limiting for authentication endpoints
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     rate.Limit // requests per second
	burst    int        // max burst size
	cleanup  time.Duration
	stop     chan struct{} // channel to stop cleanup goroutine
	now      func() time.Time
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
		now:      time.Now,
	}

	// Start cleanup goroutine to remove old visitors
	go rl.cleanupVisitors()

	return rl
}

// allow reports whether the IP may make a request now, and takes a token if so.
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	v, exists := rl.visitors[ip]
	if !exists {
		v = &visitor{limiter: rate.NewLimiter(rl.rate, rl.burst)}
		rl.visitors[ip] = v
	}
	v.lastSeen = now
	return v.limiter.AllowN(now, 1)
}

// cleanupVisitors removes idle visitors every cleanup interval until Stop.
func (rl *RateLimiter) cleanupVisitors() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.removeIdleVisitors()
		case <-rl.stop:
			return
		}
	}
}

// removeIdleVisitors forgets the IPs that haven't made a request for the cleanup interval.
func (rl *RateLimiter) removeIdleVisitors() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	for ip, v := range rl.visitors {
		if now.Sub(v.lastSeen) > rl.cleanup {
			delete(rl.visitors, ip)
		}
	}
}

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

// Middleware returns an HTTP middleware that enforces rate limiting
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(netutil.ClientIP(r)) {
			// Return 429 Too Many Requests
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(1/float64(rl.rate))))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", int(rl.rate)))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", rl.now().Add(time.Second).Unix()))

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
