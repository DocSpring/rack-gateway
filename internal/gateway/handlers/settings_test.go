package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func setupSettingsHandler(t *testing.T) (*SettingsHandler, *db.Database, rbac.RBACManager) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	settingsService := settings.NewService(database)
	handler := NewSettingsHandler(settingsService, mgr)

	return handler, database, mgr
}

// mockMFAService is a mock implementation of middleware.MFAVerifier for testing.
type mockMFAService struct {
	verifyTOTPFunc     func(*db.User, string, string, string, *int64) (*mfa.VerificationResult, error)
	verifyWebAuthnFunc func(*db.User, []byte, []byte, string, string, *int64) (*mfa.VerificationResult, error)
}

func (m *mockMFAService) VerifyTOTP(user *db.User, code string, ipAddress string, userAgent string, sessionID *int64) (*mfa.VerificationResult, error) {
	if m.verifyTOTPFunc != nil {
		return m.verifyTOTPFunc(user, code, ipAddress, userAgent, sessionID)
	}
	return &mfa.VerificationResult{MethodID: 1}, nil
}

func (m *mockMFAService) VerifyWebAuthnAssertion(user *db.User, sessionJSON []byte, credentialJSON []byte, ipAddress string, userAgent string, sessionID *int64) (*mfa.VerificationResult, error) {
	if m.verifyWebAuthnFunc != nil {
		return m.verifyWebAuthnFunc(user, sessionJSON, credentialJSON, ipAddress, userAgent, sessionID)
	}
	return &mfa.VerificationResult{MethodID: 1}, nil
}

// setupRouterWithMFAMiddleware creates a router with the MFA middleware applied.
// This is needed because the middleware is what enforces MFA, not the handlers.
func setupRouterWithMFAMiddleware(
	t *testing.T,
	method string,
	path string,
	authUser *auth.AuthUser,
	mfaService *mfa.Service,
	database *db.Database,
	mfaSettings *db.MFASettings,
	handler gin.HandlerFunc,
) *gin.Engine {
	router := gin.New()
	router.Handle(method, path, func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Set("user_email", authUser.Email)
		if authUser.Name != "" {
			c.Set("user_name", authUser.Name)
		}
		c.Next()
	}, middleware.RequireMFAForSettings(mfaService, database, mfaSettings), handler)
	return router
}

// setupRouterWithMockMFAMiddleware creates a router with mocked MFA verification.
func setupRouterWithMockMFAMiddleware(
	t *testing.T,
	method string,
	path string,
	authUser *auth.AuthUser,
	mockMFA middleware.MFAVerifier,
	database *db.Database,
	mfaSettings *db.MFASettings,
	handler gin.HandlerFunc,
) *gin.Engine {
	router := gin.New()
	router.Handle(method, path, func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Set("user_email", authUser.Email)
		if authUser.Name != "" {
			c.Set("user_name", authUser.Name)
		}
		c.Next()
	}, middleware.RequireMFAForSettings(mockMFA, database, mfaSettings), handler)
	return router
}

func TestUpdateGlobalSettings_MFAEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create admin user
	admin, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create handler and services
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	settingsService := settings.NewService(database)
	handler := NewSettingsHandler(settingsService, mgr)

	// Create MFA service
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)

	// Get MFA settings
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	// Create TOTP MFA method for testing MFAAlways
	totpKey, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", totpKey.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)

	err = database.ConfirmMFAMethod(method.ID, time.Now())
	require.NoError(t, err)

	t.Run("MFANone setting succeeds without MFA", func(t *testing.T) {
		// Create a session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		// Set MFA verified but NO MFA code
		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue - MFANone doesn't require it
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"default_vcs_provider": "github",
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// MFANone should succeed without fresh MFA code
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways setting requires MFA code", func(t *testing.T) {
		// Create a session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		// Set MFA verified but NO MFA code in request
		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// NO MFAType or MFAValue - this is the key
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied because MFAAlways requires fresh MFA code
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("Batch with mixed MFA levels uses highest (MFAAlways)", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"default_vcs_provider":       "github",    // MFANone
			"allow_destructive_actions":  true,        // MFAAlways
			"mfa_step_up_window_minutes": float64(15), // MFAStepUp
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// MFAAlways should take precedence over MFAStepUp and MFANone
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("Batch with only MFAStepUp requires step-up", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now
		// No RecentStepUpAt set - outside window

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"mfa_step_up_window_minutes":  float64(15),
			"mfa_trusted_device_ttl_days": float64(60),
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should require step-up
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "step-up", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_step_up_required", response["error"])
	})

	t.Run("MFAAlways with X-MFA-TOTP header succeeds", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		// Generate valid TOTP code
		code, err := totp.GenerateCode(totpKey.Secret(), time.Now())
		require.NoError(t, err)

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			MFAType:    "totp",
			MFAValue:   code,
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-MFA-TOTP", code)
		router.ServeHTTP(w, httpReq)

		// Should succeed with valid MFA code
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways with basic auth TOTP inline succeeds", func(t *testing.T) {
		sessionToken, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "cli"})
		require.NoError(t, err)

		now := time.Now()
		err = database.UpdateSessionMFAVerified(session.ID, now, nil)
		require.NoError(t, err)

		// Reload session with MFA verified
		session, err = database.GetSessionByID(session.ID)
		require.NoError(t, err)

		// Generate valid TOTP code
		code, err := totp.GenerateCode(totpKey.Secret(), time.Now())
		require.NoError(t, err)

		// Format: session_token.totp.123456
		passwordWithMFA := sessionToken + ".totp." + code

		// Use mock MFA service that always succeeds
		mockMFA := &mockMFAService{}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			// Simulate auth middleware extracting inline MFA
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				MFAType:    "totp",
				MFAValue:   code,
				Session:    session,
			}

			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", authUser.Email)
			c.Set("user_name", authUser.Name)
			c.Next()
		})
		router.Handle(http.MethodPut, "/admin/settings", middleware.RequireMFAForSettings(mockMFA, database, mfaSettings), handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.SetBasicAuth("convox", passwordWithMFA)
		router.ServeHTTP(w, httpReq)

		// Should succeed with valid inline MFA
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways with X-MFA-WebAuthn header succeeds", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		// Create mock WebAuthn data (base64-encoded JSON)
		webauthnData := map[string]string{
			"session_data":       `{"challenge":"test","rpId":"example.com"}`,
			"assertion_response": `{"id":"test","response":{}}`,
		}
		webauthnJSON, err := json.Marshal(webauthnData)
		require.NoError(t, err)
		webauthnValue := base64.StdEncoding.EncodeToString(webauthnJSON)

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			MFAType:    "webauthn",
			MFAValue:   webauthnValue,
		}

		// Use mock MFA service that always succeeds
		mockMFA := &mockMFAService{}
		router := setupRouterWithMockMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mockMFA, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-MFA-WebAuthn", webauthnValue)
		router.ServeHTTP(w, httpReq)

		// Should succeed with valid WebAuthn assertion
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways with basic auth WebAuthn inline succeeds", func(t *testing.T) {
		sessionToken, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "cli"})
		require.NoError(t, err)

		now := time.Now()
		err = database.UpdateSessionMFAVerified(session.ID, now, nil)
		require.NoError(t, err)

		// Reload session with MFA verified
		session, err = database.GetSessionByID(session.ID)
		require.NoError(t, err)

		// Create mock WebAuthn data (base64-encoded JSON)
		webauthnData := map[string]string{
			"session_data":       `{"challenge":"test","rpId":"example.com"}`,
			"assertion_response": `{"id":"test","response":{}}`,
		}
		webauthnJSON, err := json.Marshal(webauthnData)
		require.NoError(t, err)
		webauthnValue := base64.StdEncoding.EncodeToString(webauthnJSON)

		// Format: session_token.webauthn.base64data
		passwordWithMFA := sessionToken + ".webauthn." + webauthnValue

		// Use mock MFA service that always succeeds
		mockMFA := &mockMFAService{}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			// Simulate auth middleware extracting inline MFA
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				MFAType:    "webauthn",
				MFAValue:   webauthnValue,
				Session:    session,
			}

			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", authUser.Email)
			c.Set("user_name", authUser.Name)
			c.Next()
		})
		router.Handle(http.MethodPut, "/admin/settings", middleware.RequireMFAForSettings(mockMFA, database, mfaSettings), handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.SetBasicAuth("convox", passwordWithMFA)
		router.ServeHTTP(w, httpReq)

		// Should succeed with valid inline WebAuthn
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways with invalid TOTP code fails", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			MFAType:    "totp",
			MFAValue:   "invalid-code",
		}

		// Use mock MFA service that always fails
		mockMFA := &mockMFAService{
			verifyTOTPFunc: func(*db.User, string, string, string, *int64) (*mfa.VerificationResult, error) {
				return nil, fmt.Errorf("invalid TOTP code")
			},
		}

		router := setupRouterWithMockMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mockMFA, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied with invalid MFA code
		require.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_verification_failed", response["error"])
	})

	t.Run("MFAAlways with invalid WebAuthn assertion fails", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		// Create mock WebAuthn data
		webauthnData := map[string]string{
			"session_data":       `{"challenge":"test","rpId":"example.com"}`,
			"assertion_response": `{"id":"invalid","response":{}}`,
		}
		webauthnJSON, err := json.Marshal(webauthnData)
		require.NoError(t, err)
		webauthnValue := base64.StdEncoding.EncodeToString(webauthnJSON)

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			MFAType:    "webauthn",
			MFAValue:   webauthnValue,
		}

		// Use mock MFA service that always fails
		mockMFA := &mockMFAService{
			verifyWebAuthnFunc: func(*db.User, []byte, []byte, string, string, *int64) (*mfa.VerificationResult, error) {
				return nil, fmt.Errorf("invalid WebAuthn assertion")
			},
		}

		router := setupRouterWithMockMFAMiddleware(t, http.MethodPut, "/admin/settings", authUser, mockMFA, database, mfaSettings, handler.UpdateGlobalSettings)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied with invalid WebAuthn assertion
		require.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_verification_failed", response["error"])
	})
}

func TestDeleteGlobalSettings_MFAEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create admin user
	admin, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create handler and services
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	settingsService := settings.NewService(database)
	handler := NewSettingsHandler(settingsService, mgr)

	// Create MFA service
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)

	// Get MFA settings
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	// Create TOTP MFA method
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)

	err = database.ConfirmMFAMethod(method.ID, time.Now())
	require.NoError(t, err)

	t.Run("Delete MFAAlways setting requires MFA code", func(t *testing.T) {
		// First set the value
		_ = settingsService.SetGlobalSetting("allow_destructive_actions", true, &admin.ID)

		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodDelete, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.DeleteGlobalSettings)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodDelete, "/admin/settings?key=allow_destructive_actions", nil)
		router.ServeHTTP(w, httpReq)

		// Should be denied because MFAAlways requires fresh MFA code
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("Delete batch with MFAAlways takes precedence", func(t *testing.T) {
		// Set some values first
		_ = settingsService.SetGlobalSetting("allow_destructive_actions", true, &admin.ID)
		_ = settingsService.SetGlobalSetting("mfa_step_up_window_minutes", 15, &admin.ID)
		_ = settingsService.SetGlobalSetting("default_vcs_provider", "github", &admin.ID)

		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodDelete, "/admin/settings", authUser, mfaService, database, mfaSettings, handler.DeleteGlobalSettings)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodDelete, "/admin/settings?key=allow_destructive_actions&key=mfa_step_up_window_minutes&key=default_vcs_provider", nil)
		router.ServeHTTP(w, httpReq)

		// MFAAlways (allow_destructive_actions) should take precedence
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})
}

func TestUpdateAppSettings_MFAEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create admin user
	admin, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create handler and services
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	settingsService := settings.NewService(database)
	handler := NewSettingsHandler(settingsService, mgr)

	// Create MFA service
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)

	// Get MFA settings
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	// Create TOTP MFA method
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)

	err = database.ConfirmMFAMethod(method.ID, time.Now())
	require.NoError(t, err)

	t.Run("MFANone app setting succeeds without MFA", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/apps/:app/settings", authUser, mfaService, database, mfaSettings, handler.UpdateAppSettings)

		updates := map[string]interface{}{
			"vcs_provider": "github",
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/apps/test-app/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// MFANone should succeed without fresh MFA code
		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("MFAAlways app setting requires MFA code", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/apps/:app/settings", authUser, mfaService, database, mfaSettings, handler.UpdateAppSettings)

		updates := map[string]interface{}{
			"github_verification": false, // MFAAlways - disabling security feature
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/apps/test-app/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied because MFAAlways requires fresh MFA code
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("Batch app settings with MFAAlways takes precedence", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		now := time.Now()
		session.MFAVerifiedAt = &now

		authUser := &auth.AuthUser{
			Email:      admin.Email,
			Name:       admin.Name,
			Roles:      admin.Roles,
			IsAPIToken: false,
			Session:    session,
			// No MFAType or MFAValue
		}

		router := setupRouterWithMFAMiddleware(t, http.MethodPut, "/apps/:app/settings", authUser, mfaService, database, mfaSettings, handler.UpdateAppSettings)

		updates := map[string]interface{}{
			"vcs_provider":        "github",        // MFANone
			"protected_env_vars":  []string{"FOO"}, // MFAStepUp
			"github_verification": false,           // MFAAlways
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest(http.MethodPut, "/apps/test-app/settings", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// MFAAlways should take precedence
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})
}

func TestUpdateGlobalSettings_Boolean(t *testing.T) {
	handler, database, mgr := setupSettingsHandler(t)

	// Create a test user
	require.NoError(t, mgr.SaveUser("admin@example.com", &rbac.UserConfig{
		Name:  "Admin",
		Roles: []string{"admin"},
	}))

	tests := []struct {
		name           string
		updates        map[string]interface{}
		expectedStatus int
		expectedValues map[string]interface{}
		expectedSource settings.SettingSource
	}{
		{
			name: "set single boolean to true",
			updates: map[string]interface{}{
				"allow_destructive_actions": true,
			},
			expectedStatus: http.StatusOK,
			expectedValues: map[string]interface{}{
				"allow_destructive_actions": true,
			},
			expectedSource: settings.SourceDB,
		},
		{
			name: "set multiple settings",
			updates: map[string]interface{}{
				"allow_destructive_actions":   false,
				"mfa_trusted_device_ttl_days": float64(60), // JSON numbers are float64
			},
			expectedStatus: http.StatusOK,
			expectedValues: map[string]interface{}{
				"allow_destructive_actions":   false,
				"mfa_trusted_device_ttl_days": float64(60),
			},
			expectedSource: settings.SourceDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any previous settings
			for key := range tt.updates {
				_ = database.DeleteSetting(nil, key)
			}

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, err := json.Marshal(tt.updates)
			require.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("user_email", "admin@example.com")

			handler.UpdateGlobalSettings(c)

			require.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var result map[string]settings.Setting
				err := json.Unmarshal(w.Body.Bytes(), &result)
				require.NoError(t, err, "Response body: %s", w.Body.String())

				for key, expectedValue := range tt.expectedValues {
					setting, ok := result[key]
					require.True(t, ok, "Expected key %s in response", key)
					require.Equal(t, expectedValue, setting.Value, "Response body: %s", w.Body.String())
					require.Equal(t, tt.expectedSource, setting.Source, "Response body: %s", w.Body.String())
				}
			}
		})
	}

	// Test clearing settings (reverting to default)
	t.Run("clear settings reverts to default", func(t *testing.T) {
		// First set values
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		c.Request = httptest.NewRequest(http.MethodPut, "/admin/settings", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_email", "admin@example.com")

		handler.UpdateGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's in DB
		valueBytes, exists, err := database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotNil(t, valueBytes)

		// Now clear it with DELETE
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/admin/settings?key=allow_destructive_actions", nil)
		c.Set("user_email", "admin@example.com")

		handler.DeleteGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		// Verify it's deleted from DB
		_, exists, err = database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.False(t, exists)

		// Response should show default value with source "default"
		var result map[string]settings.Setting
		err = json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		setting := result["allow_destructive_actions"]
		require.Equal(t, false, setting.Value) // default is false
		require.Equal(t, settings.SourceDefault, setting.Source)
	})
}
