package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type settingsTestEnv struct {
	t              *testing.T
	handler        *SettingsHandler
	database       *db.Database
	rbac           rbac.Manager
	sessionManager *auth.SessionManager
	mfaService     *mfa.Service
	mfaSettings    *db.MFASettings
	admin          *db.User
	totpKey        *otp.Key
}

func newSettingsTestEnv(t *testing.T) *settingsTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, settingsService, sessionManager, mfaService, mfaSettings := setupSettingsServices(t)

	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	admin, err := database.CreateUser("admin@example.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))

	return &settingsTestEnv{
		t:              t,
		handler:        NewSettingsHandler(settingsService, mgr),
		database:       database,
		rbac:           mgr,
		sessionManager: sessionManager,
		mfaService:     mfaService,
		mfaSettings:    mfaSettings,
		admin:          admin,
		totpKey:        key,
	}
}

func (e *settingsTestEnv) newWebAuthUser(t *testing.T) (*auth.User, *db.UserSession) {
	t.Helper()
	_, session, err := e.sessionManager.CreateSession(e.admin, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)

	now := time.Now()
	session.MFAVerifiedAt = &now

	authUser := &auth.User{
		Email:      e.admin.Email,
		Name:       e.admin.Name,
		Roles:      e.admin.Roles,
		IsAPIToken: false,
		Session:    session,
	}
	return authUser, session
}

func (e *settingsTestEnv) totpCode(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCode(e.totpKey.Secret(), time.Now())
	require.NoError(t, err)
	return code
}

func (e *settingsTestEnv) markRecentStepUp(t *testing.T, session *db.UserSession, when time.Time) {
	t.Helper()
	session.RecentStepUpAt = &when
	require.NoError(t, e.database.UpdateSessionRecentStepUp(session.ID, when))
}

func (e *settingsTestEnv) performSettingsRequest(
	t *testing.T,
	method string,
	pattern string,
	requestPath string,
	handler gin.HandlerFunc,
	authUser *auth.User,
	payload interface{},
	params gin.Params,
	verifier middleware.MFAVerifier,
	modify func(*http.Request),
) *httptest.ResponseRecorder {
	t.Helper()

	if verifier == nil {
		verifier = e.mfaService
	}

	router := setupRouterWithMFAMiddleware(
		t,
		method,
		pattern,
		authUser,
		verifier,
		e.database,
		e.mfaSettings,
		handler,
		params,
	)

	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, requestPath, bodyReader)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if modify != nil {
		modify(req)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func setupSettingsServices(
	t *testing.T,
) (*db.Database, *settings.Service, *auth.SessionManager, *mfa.Service, *db.MFASettings) {
	t.Helper()

	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	settingsService := settings.NewService(database)
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	return database, settingsService, sessionManager, mfaService, mfaSettings
}

func setupSettingsHandler(t *testing.T) (*SettingsHandler, *db.Database, rbac.Manager) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

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

func (m *mockMFAService) VerifyTOTP(
	user *db.User,
	code string,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*mfa.VerificationResult, error) {
	if m.verifyTOTPFunc != nil {
		return m.verifyTOTPFunc(user, code, ipAddress, userAgent, sessionID)
	}
	return &mfa.VerificationResult{MethodID: 1}, nil
}

func (m *mockMFAService) VerifyWebAuthnAssertion(
	user *db.User,
	sessionJSON []byte,
	credentialJSON []byte,
	ipAddress string,
	userAgent string,
	sessionID *int64,
) (*mfa.VerificationResult, error) {
	if m.verifyWebAuthnFunc != nil {
		return m.verifyWebAuthnFunc(user, sessionJSON, credentialJSON, ipAddress, userAgent, sessionID)
	}
	return &mfa.VerificationResult{MethodID: 1}, nil
}

func setupRouterWithMFAMiddleware(
	t *testing.T,
	method string,
	pattern string,
	authUser *auth.User,
	mfaService middleware.MFAVerifier,
	database *db.Database,
	mfaSettings *db.MFASettings,
	handler gin.HandlerFunc,
	params gin.Params,
) *gin.Engine {
	router := gin.New()
	router.Handle(method, pattern, func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Set("user_email", authUser.Email)
		if authUser.Name != "" {
			c.Set("user_name", authUser.Name)
		}
		if len(params) > 0 {
			c.Params = append(gin.Params{}, params...)
		}
		c.Next()
	}, middleware.EnforceMFARequirements(mfaService, database, mfaSettings), handler)
	return router
}

func TestGlobalSettingsVCSDefaults_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)

	t.Run("requires step-up when none provided", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		payload := map[string]interface{}{
			"default_ci_provider": "github",
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/vcs-and-ci-defaults",
			"/api/v1/settings/vcs-and-ci-defaults",
			env.handler.UpdateGlobalVCSAndCIDefaults,
			authUser,
			payload,
			nil,
			nil,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "step-up", w.Header().Get("X-MFA-Required"))

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "mfa_step_up_required", resp["error"])
	})

	t.Run("accepts inline TOTP for step-up", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		code := env.totpCode(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = code

		payload := map[string]interface{}{
			"default_ci_provider": "github",
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/vcs-and-ci-defaults",
			"/api/v1/settings/vcs-and-ci-defaults",
			env.handler.UpdateGlobalVCSAndCIDefaults,
			authUser,
			payload,
			nil,
			nil,
			func(req *http.Request) {
				req.Header.Set("X-MFA-TOTP", code)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("succeeds with recent step-up", func(t *testing.T) {
		authUser, session := env.newWebAuthUser(t)
		env.markRecentStepUp(t, session, time.Now())

		payload := map[string]interface{}{
			"default_ci_org_slug": "example",
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/vcs-and-ci-defaults",
			"/api/v1/settings/vcs-and-ci-defaults",
			env.handler.UpdateGlobalVCSAndCIDefaults,
			authUser,
			payload,
			nil,
			nil,
			nil,
		)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestGlobalSettingsAllowDestructive_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)

	t.Run("requires inline MFA", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		payload := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions",
			env.handler.UpdateGlobalAllowDestructiveActions,
			authUser,
			payload,
			nil,
			nil,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "mfa_required", resp["error"])
	})

	t.Run("succeeds with valid TOTP", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		code := env.totpCode(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = code

		payload := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions",
			env.handler.UpdateGlobalAllowDestructiveActions,
			authUser,
			payload,
			nil,
			nil,
			func(req *http.Request) {
				req.Header.Set("X-MFA-TOTP", code)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid TOTP fails verification", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = "000000"

		mockVerifier := &mockMFAService{
			verifyTOTPFunc: func(*db.User, string, string, string, *int64) (*mfa.VerificationResult, error) {
				return nil, fmt.Errorf("invalid code")
			},
		}

		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions",
			env.handler.UpdateGlobalAllowDestructiveActions,
			authUser,
			map[string]interface{}{"allow_destructive_actions": true},
			nil,
			mockVerifier,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "mfa_verification_failed", resp["error"])
	})

	t.Run("succeeds with WebAuthn assertion", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		webauthnData := map[string]string{
			"session_data":       `{"challenge":"test","rpId":"example.com"}`,
			"assertion_response": `{"id":"test","response":{}}`,
		}
		raw, err := json.Marshal(webauthnData)
		require.NoError(t, err)
		authUser.MFAType = "webauthn"
		authUser.MFAValue = base64.StdEncoding.EncodeToString(raw)

		mockVerifier := &mockMFAService{}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions",
			env.handler.UpdateGlobalAllowDestructiveActions,
			authUser,
			map[string]interface{}{"allow_destructive_actions": true},
			nil,
			mockVerifier,
			func(req *http.Request) {
				req.Header.Set("X-MFA-WebAuthn", authUser.MFAValue)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestDeleteGlobalSettingsAllowDestructive_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)

	t.Run("requires inline MFA to delete", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		w := env.performSettingsRequest(
			t,
			http.MethodDelete,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions?key=allow_destructive_actions",
			env.handler.DeleteGlobalAllowDestructiveActions,
			authUser,
			nil,
			nil,
			nil,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))
	})

	t.Run("succeeds with TOTP code", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		code := env.totpCode(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = code

		w := env.performSettingsRequest(
			t,
			http.MethodDelete,
			"/api/v1/settings/allow-destructive-actions",
			"/api/v1/settings/allow-destructive-actions?key=allow_destructive_actions",
			env.handler.DeleteGlobalAllowDestructiveActions,
			authUser,
			nil,
			nil,
			nil,
			func(req *http.Request) {
				req.Header.Set("X-MFA-TOTP", code)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAppSettingsVCSCIDeploy_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)

	t.Run("requires inline MFA", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		payload := map[string]interface{}{
			"vcs_provider": "github",
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/apps/:app/settings/vcs-ci-deploy",
			"/api/v1/apps/test-app/settings/vcs-ci-deploy",
			env.handler.UpdateAppVCSCIDeploySettings,
			authUser,
			payload,
			gin.Params{{Key: "app", Value: "test-app"}},
			nil,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "always", w.Header().Get("X-MFA-Required"))
	})

	t.Run("accepts inline TOTP", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		code := env.totpCode(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = code

		payload := map[string]interface{}{
			"vcs_provider": "github",
		}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/apps/:app/settings/vcs-ci-deploy",
			"/api/v1/apps/test-app/settings/vcs-ci-deploy",
			env.handler.UpdateAppVCSCIDeploySettings,
			authUser,
			payload,
			gin.Params{{Key: "app", Value: "test-app"}},
			nil,
			func(req *http.Request) {
				req.Header.Set("X-MFA-TOTP", code)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAppSettingProtectedEnvVars_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)

	t.Run("requires recent step-up", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)

		payload := []string{"DATABASE_URL"}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/apps/:app/settings/protected-env-vars",
			"/api/v1/apps/test-app/settings/protected-env-vars",
			env.handler.UpdateAppProtectedEnvVars,
			authUser,
			payload,
			gin.Params{{Key: "app", Value: "test-app"}},
			nil,
			nil,
		)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "step-up", w.Header().Get("X-MFA-Required"))
	})

	t.Run("succeeds after inline step-up verification", func(t *testing.T) {
		authUser, _ := env.newWebAuthUser(t)
		code := env.totpCode(t)
		authUser.MFAType = "totp"
		authUser.MFAValue = code

		payload := []string{"DATABASE_URL"}
		w := env.performSettingsRequest(
			t,
			http.MethodPut,
			"/api/v1/apps/:app/settings/protected-env-vars",
			"/api/v1/apps/test-app/settings/protected-env-vars",
			env.handler.UpdateAppProtectedEnvVars,
			authUser,
			payload,
			gin.Params{{Key: "app", Value: "test-app"}},
			nil,
			func(req *http.Request) {
				req.Header.Set("X-MFA-TOTP", code)
			},
		)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestUpdateGlobalSettings_Boolean(t *testing.T) {
	handler, database, mgr := setupSettingsHandler(t)

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
				"mfa_trusted_device_ttl_days": float64(60),
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
			for key := range tt.updates {
				_ = database.DeleteSetting(nil, key)
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, err := json.Marshal(tt.updates)
			require.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("user_email", "admin@example.com")

			handler.UpdateGlobalSettings(c)

			require.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var result map[string]settings.Setting
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result), "Response body: %s", w.Body.String())

				for key, expectedValue := range tt.expectedValues {
					setting, ok := result[key]
					require.True(t, ok, "Expected key %s in response", key)
					require.Equal(t, expectedValue, setting.Value, "Response body: %s", w.Body.String())
					require.Equal(t, tt.expectedSource, setting.Source, "Response body: %s", w.Body.String())
				}
			}
		})
	}

	t.Run("clear settings reverts to default", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		updates := map[string]interface{}{
			"allow_destructive_actions": true,
		}
		body, err := json.Marshal(updates)
		require.NoError(t, err)

		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_email", "admin@example.com")

		handler.UpdateGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		valueBytes, exists, err := database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotNil(t, valueBytes)

		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/settings?key=allow_destructive_actions", nil)
		c.Set("user_email", "admin@example.com")

		handler.DeleteGlobalSettings(c)
		require.Equal(t, http.StatusOK, w.Code)

		_, exists, err = database.GetSetting(nil, "allow_destructive_actions")
		require.NoError(t, err)
		require.False(t, exists)

		var result map[string]settings.Setting
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		setting := result["allow_destructive_actions"]
		require.Equal(t, false, setting.Value)
		require.Equal(t, settings.SourceDefault, setting.Source)
	})
}
