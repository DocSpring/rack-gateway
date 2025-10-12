package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	mfa "github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func TestEnforceMFARequirements_AllowsInlineTOTP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin-inline-totp@example.com", "Inline Admin", []string{"admin"})
	require.NoError(t, err)
	require.NoError(t, database.SetUserMFAEnrolled(user.ID, true))

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Rack Gateway Test",
		AccountName: user.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(user.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))

	pepper := []byte("0123456789abcdef0123456789abcdef")
	mfaService, err := mfa.NewService(database, "Rack Gateway Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "localhost", "http://localhost", nil)
	require.NoError(t, err)

	settingsService := settings.NewService(database)
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	sessionManager := auth.NewSessionManager(database, "test-session-secret", time.Hour)

	newSession := func(t *testing.T) *db.UserSession {
		t.Helper()
		_, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)
		require.NotNil(t, session)

		verifiedAt := time.Now()
		require.NoError(t, sessionManager.UpdateSessionMFAVerified(session.ID, verifiedAt, nil))
		session.MFAVerifiedAt = &verifiedAt
		session.RecentStepUpAt = nil

		return session
	}

	const endpoint = "/api/v1/auth/mfa/backup-codes/regenerate"

	newRouter := func(session *db.UserSession) *gin.Engine {
		router := gin.New()
		router.POST(endpoint, func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      user.Email,
				Name:       user.Name,
				Roles:      user.Roles,
				IsAPIToken: false,
				Session:    session,
			}

			if code := strings.TrimSpace(c.GetHeader("X-MFA-TOTP")); code != "" {
				authUser.MFAType = "totp"
				authUser.MFAValue = code
			}

			ctx := context.WithValue(c.Request.Context(), auth.UserContextKey, authUser)
			c.Request = c.Request.WithContext(ctx)
			c.Set("user_email", user.Email)
			c.Set("user_name", user.Name)
			c.Next()
		}, EnforceMFARequirements(mfaService, database, mfaSettings), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		return router
	}

	t.Run("denies without inline totp", func(t *testing.T) {
		session := newSession(t)
		router := newRouter(session)

		req := httptest.NewRequest(http.MethodPost, endpoint, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "mfa_step_up_required", resp["error"])
	})

	t.Run("allows with inline totp header", func(t *testing.T) {
		session := newSession(t)
		router := newRouter(session)

		code, err := totp.GenerateCode(key.Secret(), time.Now())
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, endpoint, nil)
		req.Header.Set("X-MFA-TOTP", code)
		req.Header.Set("User-Agent", "totp-inline-test")
		req.Header.Set("X-Forwarded-For", "203.0.113.10")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, session.RecentStepUpAt, "step-up timestamp should be recorded")

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "ok", resp["status"])
	})

	t.Run("denies with enrollment-required when session never verified", func(t *testing.T) {
		_, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)
		require.NotNil(t, session)

		router := newRouter(session)
		req := httptest.NewRequest(http.MethodPost, endpoint, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Equal(t, "enrollment", w.Header().Get("X-MFA-Required"))

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, "mfa_enrollment_required", resp["error"])
	})
}
