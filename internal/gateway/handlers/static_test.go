package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestStaticHandlerRedirectsToDefault(t *testing.T) {
	handler := NewStaticHandler(&config.Config{}, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/.gateway/web/", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "filepath", Value: "/"}}

	handler.ServeStatic(c)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != DefaultWebRoute {
		t.Fatalf("expected redirect to %s, got %s", DefaultWebRoute, loc)
	}
}

func TestStaticHandlerProxiesInDev(t *testing.T) {
	received := make(chan *http.Request, 1)
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Clone(r.Context())
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("proxy"))
	})

	handler := NewStaticHandler(&config.Config{DevMode: true}, nil)
	handler.devProxy = stub

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/.gateway/web/login", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "filepath", Value: "/login"}}

	handler.ServeStatic(c)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected proxy status 418, got %d", rec.Code)
	}
	select {
	case r := <-received:
		if r.URL.Path != "/.gateway/web/login" {
			t.Fatalf("expected proxy to preserve path, got %s", r.URL.Path)
		}
	default:
		t.Fatal("expected request to hit proxy handler")
	}
}

func TestStaticHandlerServesDist(t *testing.T) {
	handler := NewStaticHandler(&config.Config{}, nil)

	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "index.html")
	assetPath := filepath.Join(tmp, "assets", "app.js")
	if err := os.WriteFile(indexPath, []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("failed to write asset: %v", err)
	}

	handler.distRoot = tmp
	handler.configureAssets()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/.gateway/web/login", nil)
	c.Request = req
	c.Params = gin.Params{{Key: "filepath", Value: "/login"}}

	handler.ServeStatic(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA route, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("unexpected body: %s", got)
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("expected no-store cache header, got %q", cache)
	}

	recAsset := httptest.NewRecorder()
	cAsset, _ := gin.CreateTestContext(recAsset)
	reqAsset := httptest.NewRequest(http.MethodGet, "/.gateway/web/assets/app.js", nil)
	cAsset.Request = reqAsset
	cAsset.Params = gin.Params{{Key: "filepath", Value: "/assets/app.js"}}

	handler.ServeStatic(cAsset)

	if recAsset.Code != http.StatusOK {
		t.Fatalf("expected 200 for asset, got %d", recAsset.Code)
	}
	if got := recAsset.Body.String(); got != "console.log('ok')" {
		t.Fatalf("unexpected asset body: %s", got)
	}
	if cache := recAsset.Header().Get("Cache-Control"); !strings.Contains(cache, "max-age=") {
		t.Fatalf("expected Cache-Control with max-age, got %q", cache)
	}
}
