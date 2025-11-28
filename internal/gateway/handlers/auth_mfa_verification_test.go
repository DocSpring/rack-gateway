package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// TestVerifyMFAUsesSharedHelper validates that VerifyMFA uses the shared verification flow
func TestVerifyMFAUsesSharedHelper(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.SetUserMFAEnrolled(user.ID, true); err != nil {
		t.Fatalf("failed to mark user enrolled: %v", err)
	}

	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	_, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
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

	// Create TOTP method for the user
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}

	method, err := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create MFA method: %v", err)
	}
	if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm MFA method: %v", err)
	}

	// Generate a valid TOTP code
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}

	settings := &db.MFASettings{RequireAllUsers: true}
	cfg := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(nil, database, cfg, sessionManager, mfaService, settings, nil, auditLogger)

	// Create test request with auth user in context
	authUser := &auth.User{
		Email:      user.Email,
		Name:       user.Name,
		Roles:      user.Roles,
		IsAPIToken: false,
		Session:    session,
	}

	reqBody := []byte(`{"code":"` + code + `","trust_device":false}`)
	c, w := newTestContext(http.MethodPost, "/api/v1/auth/mfa/verify")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/verify", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))

	// Call the handler
	handler.VerifyMFA(c)

	// Verify the response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp VerifyMFAResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.MFAVerifiedAt.IsZero() {
		t.Fatalf("expected MFAVerifiedAt to be set")
	}
	if resp.RecentStepUpExpiresAt.IsZero() {
		t.Fatalf("expected RecentStepUpExpiresAt to be set")
	}
	if resp.TrustedDeviceCookie {
		t.Fatalf("expected TrustedDeviceCookie to be false")
	}
}

// TestVerifyMFAHandlesFailure validates that the shared helper handles verification failures
func TestVerifyMFAHandlesFailure(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "User", []string{"viewer"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.SetUserMFAEnrolled(user.ID, true); err != nil {
		t.Fatalf("failed to mark user enrolled: %v", err)
	}

	sessionManager := auth.NewSessionManager(database, "test-secret", &auth.StaticTTLProvider{TTL: time.Hour})
	_, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
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

	// Create TOTP method for the user
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	if err != nil {
		t.Fatalf("failed to generate TOTP key: %v", err)
	}

	method, err := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create MFA method: %v", err)
	}
	if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
		t.Fatalf("failed to confirm MFA method: %v", err)
	}

	settings := &db.MFASettings{RequireAllUsers: true}
	cfg := &config.Config{DevMode: true}
	auditLogger := audit.NewLogger(database)
	handler := NewAuthHandler(nil, database, cfg, sessionManager, mfaService, settings, nil, auditLogger)

	// Create test request with invalid code
	authUser := &auth.User{
		Email:      user.Email,
		Name:       user.Name,
		Roles:      user.Roles,
		IsAPIToken: false,
		Session:    session,
	}

	reqBody := []byte(`{"code":"000000","trust_device":false}`)
	c, w := newTestContext(http.MethodPost, "/api/v1/auth/mfa/verify")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/mfa/verify", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))

	// Call the handler
	handler.VerifyMFA(c)

	// Verify the response indicates failure
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}

	if _, ok := errResp["error"]; !ok {
		t.Fatalf("expected error field in response")
	}
}
