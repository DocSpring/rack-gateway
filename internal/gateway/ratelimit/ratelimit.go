package ratelimit

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// RateLimiter manages per-IP rate limiting for authentication endpoints. It keeps a bucket for at most MaxClients
// clients (IPv6 grouped by /64); see buckets.
type RateLimiter struct {
	clients *buckets
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
	return &RateLimiter{clients: newBuckets(rate.Limit(requestsPerSecond), burst)}
}

// Middleware returns an HTTP middleware that enforces rate limiting
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.clients.take(netutil.ClientIP(r)) {
			perSecond := float64(rl.clients.rate)
			// Return 429 Too Many Requests
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(1/perSecond)))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", int(perSecond)))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", rl.clients.now().Add(time.Second).Unix()))

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

// PositiveEnv returns the environment variable name as a number, or def when it's unset or isn't a positive
// number.
func PositiveEnv(name string, def float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return def
	}
	return value
}

// Limiter limits an action per key (an API token or a user, say): each key may act burst times, which come back at
// perSecond. It keeps a bucket for at most MaxClients keys; see buckets.
type Limiter struct {
	keys *buckets
}

// NewLimiter allows each key burst actions, refilled at perSecond.
func NewLimiter(perSecond float64, burst int) *Limiter {
	return &Limiter{keys: newBuckets(rate.Limit(perSecond), max(burst, 1))}
}

// Allow reports whether key may act now, and counts the action if so.
func (l *Limiter) Allow(key string) bool {
	return l.keys.take(key)
}

// FailureLimiter limits each client's failed attempts (refused credentials, say), so they can't be guessed or
// used to load the database at the rate requests arrive. Successful attempts are never limited, but an attempt
// still running counts as a failure until it ends (see Begin): a burst of concurrent attempts can't all get past
// the limit before the first of them fails. A client is an IP address (see clientKey), or any other key, such as
// a credential's hash, which is used as it is.
type FailureLimiter struct {
	clients *buckets

	mu sync.Mutex
	// inFlight counts each client's attempts that have begun and not ended, by clientKey. A client is only in it
	// while it has one, so it holds at most one entry per request being handled.
	inFlight map[string]int
}

// NewFailureLimiter allows each client burst failed attempts, refilled at perSecond.
func NewFailureLimiter(perSecond float64, burst int) *FailureLimiter {
	burst = max(burst, 1)
	return &FailureLimiter{clients: newBuckets(rate.Limit(perSecond), burst), inFlight: make(map[string]int)}
}

// Begin starts an attempt from client, or reports false when the client's failed attempts are used up, counting
// each of its attempts still running as a failure. Beginning doesn't take a failed attempt: end does, when the
// attempt failed. end must be called once the attempt is over.
func (f *FailureLimiter) Begin(client string) (end func(failed bool), ok bool) {
	key := clientKey(client)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clients.tokens(client) < float64(f.inFlight[key]+1) {
		return nil, false
	}
	f.inFlight[key]++
	var once sync.Once
	return func(failed bool) {
		once.Do(func() { f.end(client, key, failed) })
	}, true
}

func (f *FailureLimiter) end(client, key string, failed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFlight[key]--; f.inFlight[key] <= 0 {
		delete(f.inFlight, key)
	}
	if failed {
		f.clients.take(client)
	}
}

// Fail counts a failed attempt from client that wasn't begun with Begin.
func (f *FailureLimiter) Fail(client string) {
	f.clients.take(client)
}
