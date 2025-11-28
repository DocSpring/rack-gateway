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

// NewAuthHandler is defined in auth.go
// trustedDeviceCookie and webOAuthStateCookie are defined in auth_helpers.go

type fakeOAuth struct {
	resp        *auth.LoginResponse
	completeErr error
	startURL    string
	startState  string
}

func (_ *fakeOAuth) StartLogin() (*auth.LoginStartResponse, error) {
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

func (f *fakeOAuth) CompleteLogin(_, _, _ string) (*auth.LoginResponse, error) {
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

	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	sessionToken, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sessionToken == "" {
		t.Fatalf("expected non-empty session token")
	}

	pepper := []byte("mfa-pepper-for-tests")
	mfaService, err := mfa.NewService(
		database,
		"Rack Gateway",
		30*time.Minute,
		10*time.Minute,
		pepper,
		"",
		"",
		"",
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("failed to init mfa service: %v", err)
	}

	settings := &db.MFASettings{RequireAllUsers: true}
	cfg := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(oauth, database, cfg, sessionManager, mfaService, settings, nil, auditLogger)

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
	if _, err := database.CreateUser("dev@example.com", "Dev", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(
		oauth,
		database,
		&config.Config{DevMode: true},
		sessionManager,
		nil,
		nil,
		nil,
		auditLogger,
	)

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
	if _, err := database.CreateUser("prod@example.com", "Prod", []string{"viewer"}); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(
		oauth,
		database,
		&config.Config{DevMode: false},
		sessionManager,
		nil,
		nil,
		nil,
		auditLogger,
	)

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
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(
		oauth,
		database,
		&config.Config{DevMode: true},
		sessionManager,
		nil,
		nil,
		nil,
		auditLogger,
	)

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
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(
		oauth,
		database,
		&config.Config{DevMode: false},
		sessionManager,
		nil,
		nil,
		nil,
		auditLogger,
	)

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
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(
		oauth,
		database,
		&config.Config{DevMode: true},
		sessionManager,
		nil,
		nil,
		nil,
		auditLogger,
	)

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

	user, mfaMethod := setupMFATestUser(t, database)
	setupTrustedDevicesForTest(t, database, user.ID)

	sessionToken := createTestSession(t, database, user)
	handler := createTestAuthHandler(database, oauth)

	c, w := setupDeleteMFARequest(t, mfaMethod.ID, sessionToken, user)

	handler.DeleteMFAMethod(c)

	verifyDeleteMFAResponse(t, w, database, user.ID)
}

func setupMFATestUser(t *testing.T, database *db.Database) (*db.User, *db.MFAMethod) {
	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.SetUserMFAEnrolled(user.ID, true); err != nil {
		t.Fatalf("failed to mark user enrolled: %v", err)
	}

	now := time.Now()
	mfaMethod, err := database.CreateMFAMethod(user.ID, "totp", "Test TOTP", "test-secret", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create mfa method: %v", err)
	}
	if err := database.ConfirmMFAMethod(mfaMethod.ID, now); err != nil {
		t.Fatalf("failed to confirm mfa method: %v", err)
	}

	return user, mfaMethod
}

func setupTrustedDevicesForTest(t *testing.T, database *db.Database, userID int64) {
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	devices := []struct {
		uuid, hash string
	}{
		{"11111111-1111-1111-1111-111111111111", "device1-token-hash"},
		{"22222222-2222-2222-2222-222222222222", "device2-token-hash"},
	}

	for i, device := range devices {
		_, err := database.CreateTrustedDevice(
			userID, device.uuid, device.hash, expiresAt,
			"127.0.0.1", fmt.Sprintf("ua-hash-%d", i+1), nil,
		)
		if err != nil {
			t.Fatalf("failed to create device%d: %v", i+1, err)
		}
	}
}

func createTestSession(t *testing.T, database *db.Database, user *db.User) string {
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	sessionToken, _, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	return sessionToken
}

func createTestAuthHandler(database *db.Database, oauth *fakeOAuth) *AuthHandler {
	pepper := []byte("mfa-pepper-for-tests")
	mfaService, _ := mfa.NewService(
		database, "Rack Gateway", 30*time.Minute, 10*time.Minute, pepper,
		"", "", "", "", nil,
	)
	settings := &db.MFASettings{RequireAllUsers: true}
	cfg := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	return NewAuthHandler(oauth, database, cfg, sessionManager, mfaService, settings, nil, auditLogger)
}

func setupDeleteMFARequest(
	_ *testing.T, methodID int64, sessionToken string, user *db.User,
) (*gin.Context, *httptest.ResponseRecorder) {
	methodIDStr := fmt.Sprintf("%d", methodID)
	c, w := newTestContext(http.MethodDelete, "/api/v1/auth/mfa/methods/"+methodIDStr)
	c.Params = []gin.Param{{Key: "methodID", Value: methodIDStr}}
	c.Request.Header.Set("Authorization", "Bearer "+sessionToken)

	authUser := &auth.User{
		Email:      user.Email,
		Name:       user.Name,
		Roles:      user.Roles,
		IsAPIToken: false,
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))

	return c, w
}

func verifyDeleteMFAResponse(t *testing.T, w *httptest.ResponseRecorder, database *db.Database, userID int64) {
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if user.MFAEnrolled {
		t.Errorf("expected user MFAEnrolled to be false after deleting last method")
	}

	devices, err := database.ListTrustedDevices(userID)
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
