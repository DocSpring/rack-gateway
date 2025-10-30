package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

func TestHostValidatorAllowsExactDomain(t *testing.T) {
	router, _ := setupMiddlewareTest(t, &config.Config{Domain: "gateway.example.com", Port: "8447"})
	driveRequest(t, router, func(req *http.Request) {
		req.Host = "gateway.example.com:8447"
		req.Header.Set("Origin", "https://gateway.example.com")
	}, http.StatusOK)
}

func TestHostValidatorRejectsSubstringDomain(t *testing.T) {
	router, _ := setupMiddlewareTest(t, &config.Config{Domain: "gateway.example.com", Port: "8447"})
	driveRequest(t, router, func(req *http.Request) {
		req.Host = "gateway-example.com"
		req.Header.Set("Origin", "https://gateway.example.com")
	}, http.StatusBadRequest, http.StatusForbidden)
}

func TestHostValidatorRejectsMismatchedOrigin(t *testing.T) {
	router, _ := setupMiddlewareTest(t, &config.Config{Domain: "gateway.example.com", Port: "8447"})
	driveRequest(t, router, func(req *http.Request) {
		req.Host = "gateway.example.com"
		req.Header.Set("Origin", "https://evil.example.com")
	}, http.StatusBadRequest, http.StatusForbidden)
}

func TestHostValidatorAllowsDevLocalhost(t *testing.T) {
	router, cancel := setupMiddlewareTest(
		t,
		&config.Config{Domain: "gateway.example.com", Port: "8447", DevMode: true},
		gin.DebugMode,
	)
	defer cancel()
	driveRequest(t, router, func(req *http.Request) {
		req.Host = "localhost:8447"
		req.Header.Set("Origin", "http://localhost:3000")
	}, http.StatusOK)
}

func TestHostValidatorAllowsKubeProbe(t *testing.T) {
	router, _ := setupMiddlewareTest(t, &config.Config{Domain: "gateway.example.com", Port: "8447"})
	driveRequest(t, router, func(req *http.Request) {
		req.Host = "internal-service"
		req.Header.Set("User-Agent", "kube-probe/1.28")
	}, http.StatusOK)
}

func setupMiddlewareTest(t *testing.T, cfg *config.Config, ginModes ...string) (*gin.Engine, func()) {
	originalMode := gin.Mode()
	targetMode := gin.ReleaseMode
	if len(ginModes) > 0 {
		targetMode = ginModes[0]
	}
	gin.SetMode(targetMode)
	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})
	restore := func() {
		gin.SetMode(originalMode)
	}
	return router, restore
}

func driveRequest(t *testing.T, router *gin.Engine, mutate func(*http.Request), allowedStatuses ...int) {
	t.Helper()

	req := httptest.NewRequest("GET", "/", nil)
	// Default headers expected by tests
	req.Header.Set("User-Agent", "Mozilla/5.0")
	mutate(req)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if len(allowedStatuses) == 0 {
		allowedStatuses = []int{http.StatusOK}
	}
	for _, status := range allowedStatuses {
		if resp.Code == status {
			return
		}
	}
	t.Fatalf("unexpected status %d, allowed: %v", resp.Code, allowedStatuses)
}

func TestSecurityHeadersAddsSentryReporting(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{
		SentryJSDsn:       "https://abc123@o75.ingest.us.sentry.io/9001",
		SentryEnvironment: "prod",
		SentryRelease:     "v5.4.3",
	}
	router.Use(SecurityHeaders(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	csp := resp.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://o75.ingest.us.sentry.io/api/9001/security/") {
		t.Fatalf("expected Sentry report URI in CSP, got %q", csp)
	}
	if !strings.Contains(csp, "sentry_environment=prod") || !strings.Contains(csp, "sentry_release=v5.4.3") {
		t.Fatalf("expected Sentry query parameters in CSP, got %q", csp)
	}
	if !strings.Contains(csp, "report-to rgw-sentry-csp") {
		t.Fatalf("expected report-to directive in CSP, got %q", csp)
	}
	if !strings.Contains(csp, "https://o75.ingest.us.sentry.io") {
		t.Fatalf("expected connect-src to include Sentry origin, got %q", csp)
	}
	if !strings.Contains(csp, "img-src 'self' data: https://o75.ingest.us.sentry.io") {
		t.Fatalf("expected img-src to allow Sentry origin, got %q", csp)
	}

	reportTo := resp.Header().Get("Report-To")
	if !strings.Contains(reportTo, "\"group\":\"rgw-sentry-csp\"") ||
		!strings.Contains(reportTo, "https://o75.ingest.us.sentry.io/api/9001/security/") {
		t.Fatalf("expected Report-To header to include Sentry endpoint, got %q", reportTo)
	}

	reportingEndpoints := resp.Header().Get("Reporting-Endpoints")
	if !strings.Contains(reportingEndpoints, "rgw-sentry-csp=") {
		t.Fatalf("expected Reporting-Endpoints header, got %q", reportingEndpoints)
	}
}

func TestRateLimitIgnoresSpoofedForwardedFor(t *testing.T) {
	origMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(origMode)

	t.Setenv("RATE_LIMIT_RPS", "1")
	t.Setenv("RATE_LIMIT_BURST", "1")

	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("failed to configure trusted proxies: %v", err)
	}
	cfg := &config.Config{}
	router.Use(RateLimit(cfg, nil))
	router.GET("/auth", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req1 := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req1.RemoteAddr = "10.0.0.1:1000"
	req1.Header.Set("X-Forwarded-For", "1.1.1.1")
	resp1 := httptest.NewRecorder()
	router.ServeHTTP(resp1, req1)
	if resp1.Code != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", resp1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req2.RemoteAddr = "10.0.0.1:2000"
	req2.Header.Set("X-Forwarded-For", "2.2.2.2")
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", resp2.Code)
	}
}
