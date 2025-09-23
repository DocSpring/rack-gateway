package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRateLimitIgnoresSpoofedForwardedFor(t *testing.T) {
	origMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(origMode)

	origRPS := os.Getenv("RATE_LIMIT_RPS")
	origBurst := os.Getenv("RATE_LIMIT_BURST")
	os.Setenv("RATE_LIMIT_RPS", "1")
	os.Setenv("RATE_LIMIT_BURST", "1")
	defer func() {
		if origRPS == "" {
			os.Unsetenv("RATE_LIMIT_RPS")
		} else {
			t := origRPS
			os.Setenv("RATE_LIMIT_RPS", t)
		}
		if origBurst == "" {
			os.Unsetenv("RATE_LIMIT_BURST")
		} else {
			t := origBurst
			os.Setenv("RATE_LIMIT_BURST", t)
		}
	}()

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
