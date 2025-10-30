package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/webauthntest"
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

	method, err := database.CreateMFAMethod(
		user.ID,
		"webauthn",
		"Test Credential",
		"",
		credential.ID,
		credential.PublicKey,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))

	mfaService, mfaSettings, sessionManager := setupMFAHelpers(t, database)
	session := confirmSession(t, sessionManager, user)

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

	middleware := EnforceMFARequirements(mfaService, database, mfaSettings)
	router := buildStepUpRouter(
		"/api/v1/api-tokens",
		session,
		user,
		middleware,
		func(authUser *auth.AuthUser, c *gin.Context) {
			authUser.MFAType = "webauthn"
			authUser.MFAValue = inlineHeader
			c.Request.Header.Set("X-MFA-WebAuthn", inlineHeader)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-tokens", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MFA-WebAuthn", inlineHeader)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	t.Logf("response body: %s", w.Body.String())
	require.Equal(
		t,
		http.StatusOK,
		w.Code,
		"EnforceMFARequirements should accept inline WebAuthn verification in the same request",
	)
}
