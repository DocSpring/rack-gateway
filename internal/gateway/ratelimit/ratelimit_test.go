package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

func TestRateLimiter(t *testing.T) {
	// Create a rate limiter with 5 requests per second, burst of 10
	rl := NewRateLimiter(5, 10)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK")) //nolint:errcheck,gosec // G104: test response
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
	time.Sleep(time.Second / 5) // Wait for 1/5 second (1 token at 5 req/sec)

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

func TestAuthEndpointsOnly(t *testing.T) {
	rl := NewRateLimiter(100, 2) // High rate limit but small burst for faster testing

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK")) //nolint:errcheck,gosec // G104: test response
	})

	// Wrap with auth-only rate limiter
	handler := rl.AuthEndpointsOnly(testHandler)

	tests := []struct {
		path        string
		method      string
		shouldLimit bool
	}{
		{"/auth/login", "POST", true},
		{"/auth/web/callback", "GET", true},
		{"/logout", "GET", true},
		{"/api/v1/tokens", "POST", true},
		{"/api/v1/users", "GET", false},
		{"/api/v1/tokens", "GET", false}, // GET is not rate limited
		{"/health", "GET", false},
		{"/apps", "GET", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %s", tt.method, tt.path), func(t *testing.T) {
			// Use unique IP per test to avoid interference
			testIP := fmt.Sprintf("10.0.%d.%d:1234", len(tt.path), len(tt.method))

			// Make 3 requests (burst is 2)
			var lastCode int
			for i := 0; i < 3; i++ {
				req := httptest.NewRequest(tt.method, tt.path, nil)
				req.RemoteAddr = testIP
				rr := httptest.NewRecorder()

				handler.ServeHTTP(rr, req)
				lastCode = rr.Code
			}

			if tt.shouldLimit {
				if lastCode != http.StatusTooManyRequests {
					t.Errorf("Expected rate limiting (429) for %s %s, got %d", tt.method, tt.path, lastCode)
				}
			} else {
				if lastCode != http.StatusOK {
					t.Errorf("Expected no rate limiting (200) for %s %s, got %d", tt.method, tt.path, lastCode)
				}
			}
		})
		// No sleep needed since we use unique IPs
	}
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
	// Create a custom rate limiter with very short cleanup interval for fast testing
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(5),
		burst:    10,
		cleanup:  50 * time.Millisecond, // Very short cleanup for fast testing
		stop:     make(chan struct{}),
	}
	defer rl.Stop() // Ensure cleanup goroutine stops

	// Start cleanup goroutine
	go rl.cleanupVisitors()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Middleware(testHandler)

	// Make a request from an IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify visitor exists
	rl.mu.RLock()
	if _, exists := rl.visitors["192.168.1.1"]; !exists {
		t.Error("Visitor should exist after request")
	}
	rl.mu.RUnlock()

	// Wait for cleanup (2x cleanup interval to ensure it runs)
	time.Sleep(110 * time.Millisecond)

	// Verify visitor was cleaned up
	rl.mu.RLock()
	if _, exists := rl.visitors["192.168.1.1"]; exists {
		t.Error("Visitor should have been cleaned up")
	}
	rl.mu.RUnlock()
}
