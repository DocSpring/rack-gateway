package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	mfa "github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnforceMFARequirements_AllowsInlineWebAuthn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	user, err := database.CreateUser("admin-inline@example.com", "Inline Admin", []string{"admin"})
	require.NoError(t, err)
	require.NoError(t, database.SetUserMFAEnrolled(user.ID, true))

	credential, err := webauthntest.GenerateMockCredential()
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(user.ID, "webauthn", "Test Credential", "", credential.ID, credential.PublicKey, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))

	pepper := []byte("0123456789abcdef0123456789abcdef")
	mfaService, err := mfa.NewService(database, "Rack Gateway Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "localhost", "http://localhost", nil)
	require.NoError(t, err)

	settingsService := settings.NewService(database)
	mfaSettings, err := settingsService.GetMFASettings()
	require.NoError(t, err)

	sessionManager := auth.NewSessionManager(database, "test-session-secret", time.Hour)
	_, session, err := sessionManager.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	verifiedAt := time.Now()
	require.NoError(t, sessionManager.UpdateSessionMFAVerified(session.ID, verifiedAt, nil))
	session.MFAVerifiedAt = &verifiedAt
	session.RecentStepUpAt = nil

	_, sessionData, err := mfaService.StartWebAuthnAssertion(user)
	require.NoError(t, err)

	assertionJSON, err := credential.GenerateAssertionForSession(sessionData, "http://localhost")
	require.NoError(t, err)

	inlinePayload := map[string]string{
		"session_data":       string(sessionData),
		"assertion_response": assertionJSON,
	}
	inlineBytes, err := json.Marshal(inlinePayload)
	require.NoError(t, err)
	inlineHeader := base64.StdEncoding.EncodeToString(inlineBytes)

	body := map[string]interface{}{
		"name":        "inline-token",
		"permissions": []string{"convox:app:list"},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	router := gin.New()
	router.POST("/api/v1/admin/tokens", func(c *gin.Context) {
		authUser := &auth.AuthUser{
			Email:      user.Email,
			Name:       user.Name,
			Roles:      user.Roles,
			IsAPIToken: false,
			Session:    session,
			MFAType:    "webauthn",
			MFAValue:   inlineHeader,
		}
		ctx := context.WithValue(c.Request.Context(), auth.UserContextKey, authUser)
		c.Request = c.Request.WithContext(ctx)
		c.Request.Header.Set("X-MFA-WebAuthn", inlineHeader)
		c.Set("user_email", user.Email)
		c.Next()
	}, EnforceMFARequirements(mfaService, database, mfaSettings), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tokens", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MFA-WebAuthn", inlineHeader)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	t.Logf("response body: %s", w.Body.String())
	require.Equal(t, http.StatusOK, w.Code, "EnforceMFARequirements should accept inline WebAuthn verification in the same request")
}
