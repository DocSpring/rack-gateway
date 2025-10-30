package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
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
	return &auth.LoginResponse{Email: "user@example.com", Name: "User"}, nil
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

func TestHandlePostLoginMFAClearsStaleTrustedDevice(t *testing.T) {
	oauth := &fakeOAuth{}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.SetUserMFAEnrolled(user.ID, true); err != nil {
		t.Fatalf("failed to mark user enrolled: %v", err)
	}
	if user, err = database.GetUser("user@example.com"); err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}

	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	sessionToken, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sessionToken == "" {
		t.Fatalf("expected non-empty session token")
	}

	pepper := []byte("mfa-pepper-for-tests")
	mfaService, err := mfa.NewService(database, "Rack Gateway", 30*time.Minute, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to init mfa service: %v", err)
	}

	settings := &db.MFASettings{RequireAllUsers: true}
	config := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, config, sessionManager, mfaService, settings, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/callback")
	c.Request.AddCookie(&http.Cookie{Name: trustedDeviceCookie, Value: "stale-token"})
	c.Request.Header.Set("User-Agent", "Playwright Test")

	if err := handler.handlePostLoginMFA(c, user, session); err != nil {
		t.Fatalf("handlePostLoginMFA returned error: %v", err)
	}

	res := w.Result()
	cookie := findCookie(res, trustedDeviceCookie)
	if cookie == nil {
		t.Fatalf("expected trusted device cookie to be cleared")
	}
	if cookie.Value != "" {
		t.Fatalf("expected cleared cookie value, got %q", cookie.Value)
	}
	if cookie.MaxAge >= 0 && !cookie.Expires.Before(time.Now()) {
		t.Fatalf("expected trusted device cookie to expire immediately")
	}
	if session.MFAVerifiedAt != nil {
		t.Fatalf("expected session to remain unverified when trusted device is stale")
	}
}

func TestWebLoginCallbackSetsCookieInDev(t *testing.T) {
	oauth := &fakeOAuth{resp: &auth.LoginResponse{Email: "dev@example.com", Name: "Dev"}}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	if _, err := database.CreateUser("dev@example.com", "Dev", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager, nil, nil, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/callback?code=abc&state=state")
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
	oauth := &fakeOAuth{resp: &auth.LoginResponse{Email: "prod@example.com", Name: "Prod"}}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })
	if _, err := database.CreateUser("prod@example.com", "Prod", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: false}, sessionManager, nil, nil, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/callback?code=abc&state=state")
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
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager, nil, nil, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/logout")
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
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: false}, sessionManager, nil, nil, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/login")
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
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, &config.Config{DevMode: true}, sessionManager, nil, nil, nil, auditLogger)

	c, w := newTestContext(http.MethodGet, "/api/v1/auth/web/callback?code=abc&state=other")
	c.Request.AddCookie(&http.Cookie{Name: webOAuthStateCookie, Value: "state"})
	handler.WebLoginCallback(c)

	res := w.Result()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", res.StatusCode)
	}
	loc, _ := res.Location()
	if loc == nil || !strings.Contains(loc.Path, "/auth/error") {
		t.Fatalf("expected redirect to auth error page, got %v", loc)
	}
	if !strings.Contains(loc.RawQuery, "message=Invalid") {
		t.Fatalf("expected error message in query, got %v", loc.RawQuery)
	}
}

func TestDeleteMFAMethodClearsTrustedDevicesWhenFullyDisabled(t *testing.T) {
	oauth := &fakeOAuth{}
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create user with MFA enrolled
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.SetUserMFAEnrolled(user.ID, true); err != nil {
		t.Fatalf("failed to mark user enrolled: %v", err)
	}

	// Create MFA method
	now := time.Now()
	mfaMethod, err := database.CreateMFAMethod(user.ID, "totp", "Test TOTP", "test-secret", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create mfa method: %v", err)
	}
	// Confirm the method
	if err := database.ConfirmMFAMethod(mfaMethod.ID, now); err != nil {
		t.Fatalf("failed to confirm mfa method: %v", err)
	}

	// Create trusted devices
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	_, err = database.CreateTrustedDevice(user.ID, "11111111-1111-1111-1111-111111111111", "device1-token-hash", expiresAt, "127.0.0.1", "ua-hash-1", nil)
	if err != nil {
		t.Fatalf("failed to create device1: %v", err)
	}
	_, err = database.CreateTrustedDevice(user.ID, "22222222-2222-2222-2222-222222222222", "device2-token-hash", expiresAt, "127.0.0.1", "ua-hash-2", nil)
	if err != nil {
		t.Fatalf("failed to create device2: %v", err)
	}

	// Create session
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	sessionToken, _, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Setup MFA service and handler
	pepper := []byte("mfa-pepper-for-tests")
	mfaService, err := mfa.NewService(database, "Rack Gateway", 30*time.Minute, 10*time.Minute, pepper, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("failed to init mfa service: %v", err)
	}

	settings := &db.MFASettings{RequireAllUsers: true}
	config := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, config, sessionManager, mfaService, settings, nil, auditLogger)

	// Setup request context
	methodIDStr := fmt.Sprintf("%d", mfaMethod.ID)
	c, w := newTestContext(http.MethodDelete, "/api/v1/auth/mfa/methods/"+methodIDStr)
	c.Params = []gin.Param{{Key: "methodID", Value: methodIDStr}}
	c.Request.Header.Set("Authorization", "Bearer "+sessionToken)

	// Set auth user in context
	authUser := &auth.AuthUser{
		Email:      user.Email,
		Name:       user.Name,
		Roles:      user.Roles,
		IsAPIToken: false,
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))

	// Delete the MFA method
	handler.DeleteMFAMethod(c)

	// Check response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify user is no longer MFA enrolled
	user, err = database.GetUser(user.Email)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if user.MFAEnrolled {
		t.Errorf("expected user MFAEnrolled to be false after deleting last method")
	}

	// Verify all trusted devices were revoked
	devices, err := database.ListTrustedDevices(user.ID)
	if err != nil {
		t.Fatalf("failed to list trusted devices: %v", err)
	}
	for _, device := range devices {
		if device.RevokedAt == nil {
			t.Errorf("expected device %d to be revoked, but RevokedAt is nil", device.ID)
		}
		if device.RevokedReason != "mfa_disabled" {
			t.Errorf("expected revoked reason 'mfa_disabled', got %q", device.RevokedReason)
		}
	}
}
