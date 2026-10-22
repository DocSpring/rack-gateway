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
	clock := time.Now()
	rl.clients.now = func() time.Time { return clock }

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

// A client whose bucket has refilled is forgotten, which changes nothing; one still short of tokens is kept.
func TestSweepForgetsOnlyRefilledClients(t *testing.T) {
	rl := NewRateLimiter(5, 10)
	clock := time.Now()
	rl.clients.now = func() time.Time { return clock }
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	sweepAt := func(at time.Time) bool {
		rl.clients.mu.Lock()
		defer rl.clients.mu.Unlock()
		rl.clients.sweep(at)
		_, exists := rl.clients.clients["192.168.1.1"]
		return exists
	}

	// One token short until a fifth of a second has passed.
	require.True(t, sweepAt(clock.Add(time.Second/10)), "a client still short of tokens must be kept")
	require.False(t, sweepAt(clock.Add(time.Second)), "a client whose bucket has refilled must be forgotten")
}

// Past MaxClients, new clients share one overflow bucket: memory stays bounded, and they're still limited.
func TestClientsPastTheCapShareTheOverflowBucket(t *testing.T) {
	b := newBuckets(1, 2)
	b.max = 3
	clock := time.Now()
	b.now = func() time.Time { return clock }

	for i := 1; i <= 3; i++ {
		require.True(t, b.take(fmt.Sprintf("192.0.2.%d", i)))
	}
	require.Len(t, b.clients, 3)

	// Two more clients use up the overflow bucket between them, and aren't added.
	require.True(t, b.take("198.51.100.1"))
	require.True(t, b.take("198.51.100.2"))
	require.False(t, b.take("198.51.100.3"))
	require.Len(t, b.clients, 3)

	// Clients that have a bucket keep it.
	require.True(t, b.take("192.0.2.1"))

	// Once their buckets refill, a sweep (at most once a second while full) makes room.
	clock = clock.Add(2 * time.Second)
	require.True(t, b.take("198.51.100.3"))
	require.Len(t, b.clients, 1)
}

func TestClientKeyGroupsIPv6By64(t *testing.T) {
	require.Equal(t, "2001:db8:1:2::/64", clientKey("2001:db8:1:2::1"))
	require.Equal(t, "2001:db8:1:2::/64", clientKey("2001:db8:1:2:ffff:ffff:ffff:ffff"))
	require.Equal(t, "2001:db8:1:2::/64", clientKey("2001:db8:1:2::5%eth0"))
	require.Equal(t, "2001:db8:1:3::/64", clientKey("2001:db8:1:3::1"))
	require.Equal(t, "192.0.2.1", clientKey("192.0.2.1"))
	require.Equal(t, "192.0.2.1", clientKey("::ffff:192.0.2.1"))
	require.Equal(t, "not-an-ip", clientKey("not-an-ip"))

	// Every address in a /64 shares a bucket.
	b := newBuckets(1, 1)
	require.True(t, b.take("2001:db8:1:2::1"))
	require.False(t, b.take("2001:db8:1:2::2"))
	require.True(t, b.take("2001:db8:1:3::1"))
}

// blocked reports whether the client at ip can't begin an attempt, without leaving one running.
func blocked(f *FailureLimiter, ip string) bool {
	end, ok := f.Begin(ip)
	if ok {
		end(false)
	}
	return !ok
}

func TestFailureLimiter(t *testing.T) {
	f := NewFailureLimiter(0.5, 3)
	clock := time.Now()
	f.clients.now = func() time.Time { return clock }

	// An attempt that didn't fail doesn't count, or add the client.
	for i := 0; i < 10; i++ {
		require.False(t, blocked(f, "203.0.113.7"))
	}
	require.Empty(t, f.clients.clients)
	require.Empty(t, f.inFlight)

	for i := 0; i < 3; i++ {
		end, ok := f.Begin("203.0.113.7")
		require.True(t, ok)
		end(true)
		end(true) // an attempt ends once
	}
	require.True(t, blocked(f, "203.0.113.7"))
	require.False(t, blocked(f, "203.0.113.8"), "another client isn't blocked")

	// One failed attempt comes back every two seconds.
	clock = clock.Add(2 * time.Second)
	require.False(t, blocked(f, "203.0.113.7"))
	f.Fail("203.0.113.7")
	require.True(t, blocked(f, "203.0.113.7"))

	// With the map full, a client without a bucket is blocked when the shared overflow bucket is used up.
	f.clients.max = 1
	require.False(t, blocked(f, "198.51.100.1"))
	for i := 0; i < 3; i++ {
		f.Fail("198.51.100.1")
	}
	require.True(t, blocked(f, "198.51.100.2"))
}

// Attempts still running count as failures, so a burst of concurrent attempts can't all begin before the first of
// them fails: only as many as the client has failed attempts left run at once.
func TestFailureLimiterCountsAttemptsInFlight(t *testing.T) {
	f := NewFailureLimiter(0.5, 3)
	clock := time.Now()
	f.clients.now = func() time.Time { return clock }

	var running []func(bool)
	for i := 0; i < 3; i++ {
		end, ok := f.Begin("203.0.113.7")
		require.True(t, ok)
		running = append(running, end)
	}
	_, ok := f.Begin("203.0.113.7")
	require.False(t, ok, "three attempts are running, and each may fail")
	require.False(t, blocked(f, "203.0.113.8"), "another client isn't blocked")

	running[0](false)
	end, ok := f.Begin("203.0.113.7")
	require.True(t, ok, "an attempt that succeeded gives its place back")
	end(true)
	running[1](true)
	running[2](false)

	// Two failed attempts are used; one is left.
	end, ok = f.Begin("203.0.113.7")
	require.True(t, ok)
	_, ok = f.Begin("203.0.113.7")
	require.False(t, ok)
	end(false)
	require.Empty(t, f.inFlight)
}

func TestLimiterLimitsEachKey(t *testing.T) {
	l := NewLimiter(1.0/60, 2)
	clock := time.Now()
	l.keys.now = func() time.Time { return clock }

	require.True(t, l.Allow("token:1"))
	require.True(t, l.Allow("token:1"))
	require.False(t, l.Allow("token:1"))
	require.True(t, l.Allow("token:2"), "another key has its own bucket")

	// One action comes back each minute.
	clock = clock.Add(time.Minute)
	require.True(t, l.Allow("token:1"))
	require.False(t, l.Allow("token:1"))
}
