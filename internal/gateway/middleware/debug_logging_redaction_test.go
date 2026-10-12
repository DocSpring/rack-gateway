package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/debuglog"
)

// Turning on debug logging must not put credentials or secrets in the logs: session and API tokens, cookies, CSRF
// tokens, MFA codes, the login code in a redirect, or the values in an env change.
func TestDebugLoggingRedactsCredentialsAndSecrets(t *testing.T) {
	logs := debuglog.Capture(t, "http.*")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DebugLogging(nil))
	var received string
	router.PUT("/api/v1/apps/web/env", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		received = string(body)
		c.Header("Set-Cookie", "session_token=hidden-response-cookie; HttpOnly")
		c.Header("Location", "http://127.0.0.1:54321/callback?code=hidden-login-code&state=hidden-cli-state")
		c.Status(http.StatusOK)
	})

	body := `{"set":{"DATABASE_URL":"postgres://hidden-env-value"},"remove":["OLD_KEY"],` +
		`"code":"hidden-mfa-code","password":"hidden-password","state":"hidden-login-state",` +
		`"methods":[{"api_token":"hidden-api-token","label":"visible-label"}],"name":"visible-name","limit":25}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/web/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer hidden-session-token")
	req.Header.Set("Cookie", "session_token=hidden-cookie")
	req.Header.Set("X-CSRF-Token", "hidden-csrf")
	req.Header.Set("X-MFA-TOTP", "hidden-totp")
	req.Header.Set("X-MFA-WebAuthn", "hidden-assertion")
	req.Header.Set("X-Postmark-Server-Token", "hidden-server-token")
	req.Header.Set("X-Request-ID", "visible-request-id")
	router.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, body, received, "the handler still gets the whole body")
	out := logs.String()
	for _, logged := range []string{
		"PUT /api/v1/apps/web/env",
		"X-Request-Id: visible-request-id",
		"Authorization: [REDACTED]",
		"Cookie: [REDACTED]",
		"X-Mfa-Totp: [REDACTED]",
		"Set-Cookie: [REDACTED]",
		"Location: http://127.0.0.1:54321/callback?code=%5BREDACTED%5D&state=%5BREDACTED%5D",
		`"DATABASE_URL":"[REDACTED]"`,
		`"remove":["OLD_KEY"]`,
		`"label":"visible-label"`,
		`"name":"visible-name"`,
		`"limit":25`,
	} {
		require.Contains(t, out, logged)
	}
	require.NotContains(t, out, "hidden-")
}
