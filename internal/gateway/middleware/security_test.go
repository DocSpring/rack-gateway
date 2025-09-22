package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

func TestHostValidatorAllowsExactDomain(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	cfg := &config.Config{Domain: "gateway.example.com", Port: "8447"}
	router.Use(SecurityHeaders(cfg))
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway.example.com:8447"
	req.Header.Set("Origin", "https://gateway.example.com")
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
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway-example.com"
	req.Header.Set("Origin", "https://gateway.example.com")
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
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gateway.example.com"
	req.Header.Set("Origin", "https://evil.example.com")
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
	router.Use(OriginValidator(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:8447"
	req.Header.Set("Origin", "http://localhost:3000")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("expected 200 in dev mode, got %d", resp.Code)
	}
}
