package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
)

type fakeOAuth struct {
	resp        *auth.LoginResponse
	completeErr error
	startURL    string
	startState  string
}

func (f *fakeOAuth) StartLogin() (*auth.LoginStartResponse, error) {
	return &auth.LoginStartResponse{AuthURL: "http://example.com", State: "state", CodeVerifier: "verifier"}, nil
}

func (f *fakeOAuth) StartWebLogin() (string, string) {
	url := f.startURL
	if url == "" {
		url = "http://example.com"
	}
	state := f.startState
	if state == "" {
		state = "state"
	}
	return url, state
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
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	if _, err := database.CreateUser("dev@example.com", "Dev", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager)

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/callback?code=abc&state=state")
	c.Request.AddCookie(&http.Cookie{Name: webOAuthStateCookie, Value: "state"})
	handler.WebLoginCallback(c)

	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", res.StatusCode)
	}

	cookie := findCookie(res, "session_token")
	if cookie == nil {
		t.Fatalf("expected session_token cookie to be set")
	}
	if cookie.Value == "" {
		t.Fatalf("expected cookie value to be non-empty")
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
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	if _, err := database.CreateUser("prod@example.com", "Prod", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: false}, sessionManager)

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/callback?code=abc&state=state")
	c.Request.AddCookie(&http.Cookie{Name: webOAuthStateCookie, Value: "state", Secure: true})
	handler.WebLoginCallback(c)

	res := w.Result()
	cookie := findCookie(res, "session_token")
	if cookie == nil {
		t.Fatalf("expected session_token cookie to be set")
	}
	if cookie.Value == "" {
		t.Fatalf("expected cookie value to be non-empty")
	}
	if !cookie.Secure {
		t.Fatalf("expected secure cookie when not in dev mode")
	}
}

func TestWebLogoutClearsCookie(t *testing.T) {
	oauth := &fakeOAuth{}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager)

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

func TestWebLoginStartSetsStateCookie(t *testing.T) {
	oauth := &fakeOAuth{startURL: "http://idp.example.com/login", startState: "abc123"}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: false}, sessionManager)

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/login")
	handler.WebLoginStart(c)

	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", res.StatusCode)
	}
	cookie := findCookie(res, webOAuthStateCookie)
	if cookie == nil {
		t.Fatalf("expected %s cookie to be set", webOAuthStateCookie)
	}
	if cookie.Value != "abc123" {
		t.Fatalf("expected cookie value abc123, got %s", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected HttpOnly cookie")
	}
	if !cookie.Secure {
		t.Fatalf("expected secure cookie when not in dev mode")
	}
}

func TestWebLoginCallbackRejectsInvalidState(t *testing.T) {
	oauth := &fakeOAuth{}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager)

	c, w := newTestContext(http.MethodGet, "/.gateway/api/auth/web/callback?code=abc&state=other")
	c.Request.AddCookie(&http.Cookie{Name: webOAuthStateCookie, Value: "state"})
	handler.WebLoginCallback(c)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 status, got %d", res.StatusCode)
	}
}
