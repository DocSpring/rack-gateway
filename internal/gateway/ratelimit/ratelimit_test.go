package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

func TestRateLimiter(t *testing.T) {
	// Create a rate limiter with 5 requests per second, burst of 10
	rl := NewRateLimiter(5, 10)
	defer rl.Stop()
	clock := time.Now()
	rl.now = func() time.Time { return clock }

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with rate limiter middleware
	handler := rl.Middleware(testHandler)

	// Test 1: First 10 requests should succeed (burst capacity)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, rr.Code)
		}
	}

	// Test 2: 11th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Request 11: expected status 429, got %d", rr.Code)
	}

	// Test 3: Different IP should not be affected
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:1234"
	rr2 := httptest.NewRecorder()

	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("Different IP request: expected status 200, got %d", rr2.Code)
	}

	// Test 4: After waiting, requests should succeed again
	clock = clock.Add(time.Second / 5) // 1/5 second later: 1 token at 5 req/sec

	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.1:1234"
	rr3 := httptest.NewRecorder()

	handler.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Errorf("After waiting: expected status 200, got %d", rr3.Code)
	}
}

func TestRateLimiterKeysOnResolvedClientIPNotForwardingHeaders(t *testing.T) {
	rl := NewRateLimiter(100, 1)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serve := func(clientIP, forwardedFor string) int {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.2.87.202:4321"
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		if clientIP != "" {
			req = req.WithContext(netutil.WithClientIP(req.Context(), clientIP))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// A forged X-Forwarded-For doesn't buy a fresh bucket: without a resolved client IP the peer is used.
	require.Equal(t, http.StatusOK, serve("", "1.1.1.1"))
	require.Equal(t, http.StatusTooManyRequests, serve("", "2.2.2.2"))

	// Clients the router resolved behind a trusted proxy each get their own bucket.
	require.Equal(t, http.StatusOK, serve("100.85.250.16", ""))
	require.Equal(t, http.StatusOK, serve("100.107.53.108", ""))
	require.Equal(t, http.StatusTooManyRequests, serve("100.85.250.16", ""))
}

func TestConcurrentRequests(t *testing.T) {
	rl := NewRateLimiter(10, 20)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(testHandler)

	// Test concurrent requests from different IPs
	var wg sync.WaitGroup
	rateLimited := make(map[string]bool)
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ip := fmt.Sprintf("192.168.1.%d:1234", id)
			limited := false

			// Each goroutine makes 25 requests
			for j := 0; j < 25; j++ {
				req := httptest.NewRequest("GET", "/test", nil)
				req.RemoteAddr = ip
				rr := httptest.NewRecorder()

				handler.ServeHTTP(rr, req)

				if rr.Code == http.StatusTooManyRequests {
					limited = true
				}
			}

			mu.Lock()
			rateLimited[ip] = limited
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Each IP should have been rate limited (25 requests > 20 burst)
	for ip, limited := range rateLimited {
		if !limited {
			t.Errorf("IP %s was not rate limited after 25 requests", ip)
		}
	}
}

func TestRateLimiterHeaders(t *testing.T) {
	rl := NewRateLimiter(5, 1) // 5 per second, burst of 1 for immediate limiting

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(testHandler)

	// First request should succeed
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Second request should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status 429, got %d", rr2.Code)
	}

	// Check rate limit headers
	if retryAfter := rr2.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("Missing Retry-After header")
	}

	if limit := rr2.Header().Get("X-RateLimit-Limit"); limit != "5" {
		t.Errorf("Expected X-RateLimit-Limit: 5, got %s", limit)
	}

	if remaining := rr2.Header().Get("X-RateLimit-Remaining"); remaining != "0" {
		t.Errorf("Expected X-RateLimit-Remaining: 0, got %s", remaining)
	}

	if reset := rr2.Header().Get("X-RateLimit-Reset"); reset == "" {
		t.Error("Missing X-RateLimit-Reset header")
	}

	// Check response body
	body := rr2.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

func TestVisitorCleanup(t *testing.T) {
	rl := NewRateLimiter(5, 10)
	defer rl.Stop()
	clock := time.Now()
	rl.now = func() time.Time { return clock }

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	visiting := func() bool {
		rl.mu.RLock()
		defer rl.mu.RUnlock()
		_, exists := rl.visitors["192.168.1.1"]
		return exists
	}

	// Seen within the cleanup interval: kept.
	clock = clock.Add(rl.cleanup)
	rl.removeIdleVisitors()
	require.True(t, visiting(), "a visitor seen within the cleanup interval must be kept")

	// Idle for longer: removed.
	clock = clock.Add(time.Second)
	rl.removeIdleVisitors()
	require.False(t, visiting(), "an idle visitor must be removed")
}
