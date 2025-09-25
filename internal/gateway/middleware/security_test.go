package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

func TestHostValidatorAllowsExactDomain(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447"}
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway.example.com:8447"
	req.Header.Set("Origin", "https://gateway.example.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func TestHostValidatorRejectsSubstringDomain(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447"}
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway-example.com"
	req.Header.Set("Origin", "https://gateway.example.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code < 400 {
		t.Fatalf("expected >=400, got %d", resp.Code)
	}
}

func TestHostValidatorRejectsMismatchedOrigin(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447"}
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway.example.com"
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code < 400 {
		t.Fatalf("expected >=400, got %d", resp.Code)
	}
}

func TestHostValidatorAllowsDevLocalhost(t *testing.T) {
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447", DevMode: true}
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:8447"
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("expected 200 in dev mode, got %d", resp.Code)
	}
}

func TestHostValidatorAllowsKubeProbe(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447"}
	router.Use(SecurityHeaders(cfg))
	router.Use(HostValidator(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "internal-service"
	req.Header.Set("User-Agent", "kube-probe/1.28")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
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
	if !strings.Contains(csp, "report-to cgw-sentry-csp") {
		t.Fatalf("expected report-to directive in CSP, got %q", csp)
	}
	if !strings.Contains(csp, "https://o75.ingest.us.sentry.io") {
		t.Fatalf("expected connect-src to include Sentry origin, got %q", csp)
	}

	reportTo := resp.Header().Get("Report-To")
	if !strings.Contains(reportTo, "\"group\":\"cgw-sentry-csp\"") || !strings.Contains(reportTo, "https://o75.ingest.us.sentry.io/api/9001/security/") {
		t.Fatalf("expected Report-To header to include Sentry endpoint, got %q", reportTo)
	}

	reportingEndpoints := resp.Header().Get("Reporting-Endpoints")
	if !strings.Contains(reportingEndpoints, "cgw-sentry-csp=") {
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
	router.Use(RateLimit(cfg))
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
