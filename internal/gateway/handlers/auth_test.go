package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
)

type fakeOAuth struct {
	resp        *auth.LoginResponse
	completeErr error
	startURL    string
}

func (f *fakeOAuth) StartLogin() (*auth.LoginStartResponse, error) {
	return &auth.LoginStartResponse{AuthURL: "http://example.com", State: "state", CodeVerifier: "verifier"}, nil
}

func (f *fakeOAuth) StartWebLogin() string {
	if f.startURL != "" {
		return f.startURL
	}
	return "http://example.com"
}

func (f *fakeOAuth) CompleteLogin(code, state, codeVerifier string) (*auth.LoginResponse, error) {
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &auth.LoginResponse{Token: "token", Email: "user@example.com", Name: "User", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func newTestContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, target, nil)
	c.Request = req
	return c, w
}

func findCookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestWebLoginCallbackSetsCookieInDev(t *testing.T) {
	oauth := &fakeOAuth{resp: &auth.LoginResponse{Token: "dev-token", Email: "dev@example.com", Name: "Dev", ExpiresAt: time.Now().Add(time.Hour)}}
	handler := NewAuthHandler(oauth, nil, &config.Config{DevMode: true})

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/callback?code=abc&state=state")
	handler.WebLoginCallback(c)

	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", res.StatusCode)
	}

	cookie := findCookie(res, "session_token")
	if cookie == nil {
		t.Fatalf("expected session_token cookie to be set")
	}
	if cookie.Value != "dev-token" {
		t.Fatalf("expected cookie value dev-token, got %s", cookie.Value)
	}
	if cookie.Secure {
		t.Fatalf("expected insecure cookie in dev mode")
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected HttpOnly cookie")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite Lax, got %v", cookie.SameSite)
	}
}

func TestWebLoginCallbackSetsCookieSecureInProd(t *testing.T) {
	oauth := &fakeOAuth{resp: &auth.LoginResponse{Token: "prod-token", Email: "prod@example.com", Name: "Prod", ExpiresAt: time.Now().Add(time.Hour)}}
	handler := NewAuthHandler(oauth, nil, &config.Config{DevMode: false})

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/callback?code=abc&state=state")
	handler.WebLoginCallback(c)

	res := w.Result()
	cookie := findCookie(res, "session_token")
	if cookie == nil {
		t.Fatalf("expected session_token cookie to be set")
	}
	if !cookie.Secure {
		t.Fatalf("expected secure cookie when not in dev mode")
	}
}

func TestWebLogoutClearsCookie(t *testing.T) {
	oauth := &fakeOAuth{}
	handler := NewAuthHandler(oauth, nil, &config.Config{DevMode: true})

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/logout")
	handler.WebLogout(c)

	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", res.StatusCode)
	}
	cookie := findCookie(res, "session_token")
	if cookie == nil {
		t.Fatalf("expected session_token cookie to be cleared")
	}
	if cookie.Value != "" {
		t.Fatalf("expected cleared cookie value, got %q", cookie.Value)
	}
	if cookie.MaxAge >= 0 && !cookie.Expires.Before(time.Now()) {
		t.Fatalf("expected cookie to expire immediately")
	}
}
