package middleware

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/httputil"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// enableHTTPDebugLogging turns on the HTTP debug topics for the test and returns what gets logged.
func enableHTTPDebugLogging(t *testing.T) *bytes.Buffer {
	t.Helper()
	// Cleanups run last-registered first, so each env var is restored before the logger re-reads the env. LOG_LEVEL
	// is set to info first: the logger keeps its level when LOG_LEVEL is unset, so it must re-read info before the
	// original (usually unset) LOG_LEVEL comes back, or later tests would log at debug.
	t.Cleanup(gtwlog.Reload)
	t.Setenv("LOG_LEVEL", "info")
	t.Cleanup(gtwlog.Reload)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DEBUG_TOPICS", "http.*")
	gtwlog.Reload()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &logs
}

// Turning on debug logging must not put credentials or secrets in the logs: session and API tokens, cookies, CSRF
// tokens, MFA codes, or the values in an env change.
func TestDebugLoggingRedactsCredentialsAndSecrets(t *testing.T) {
	logs := enableHTTPDebugLogging(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(DebugLogging(nil))
	var received string
	router.PUT("/api/v1/apps/web/env", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		received = string(body)
		c.Header("Set-Cookie", "session_token=hidden-response-cookie; HttpOnly")
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

func TestSensitiveHeaders(t *testing.T) {
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-Csrf-Token", "X-Mfa-Totp",
		"X-Mfa-Webauthn", "X-Mfa-Required", "X-Postmark-Server-Token", "X-Api-Key", "X-Client-Secret",
		"X-Admin-Password",
	} {
		require.True(t, isSensitiveHeader(name), name)
	}
	for _, name := range []string{"Accept", "Content-Type", "User-Agent", "X-Request-Id", "X-Forwarded-For"} {
		require.False(t, isSensitiveHeader(name), name)
	}
}

func TestRedactJSONForLog(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{
			`{"login_code":"x","code_verifier":"x","backup_codes":["x"],"pin":1234}`,
			`{"backup_codes":"[REDACTED]","code_verifier":"[REDACTED]","login_code":"[REDACTED]","pin":"[REDACTED]"}`,
		},
		{
			`{"session_data":"x","assertion_response":{"id":"x"},"yubi_otp":"x","trusted_device_cookie":"x"}`,
			`{"assertion_response":"[REDACTED]","session_data":"[REDACTED]","trusted_device_cookie":"[REDACTED]",` +
				`"yubi_otp":"[REDACTED]"}`,
		},
		{`[{"secret":"x","id":12345678901234567890}]`, `[{"id":12345678901234567890,"secret":"[REDACTED]"}]`},
		{`{"env":["DATABASE_URL=x"]}`, `{"env":"[REDACTED]"}`},
		{`{"set":{}, "remove":[]}`, `{"remove":[],"set":{}}`},
		{`"just a string"`, `"just a string"`},
	}
	for _, tt := range tests {
		got, ok := redactJSONForLog([]byte(tt.body))
		require.True(t, ok, tt.body)
		require.JSONEq(t, tt.want, got, tt.body)
	}

	for _, body := range []string{`{"password":"x"`, `password=x`, `{"a":1} {"password":"x"}`, ``} {
		_, ok := redactJSONForLog([]byte(body))
		require.False(t, ok, body)
	}
}

// A body that can't be redacted isn't logged: the start of a long one could be cut inside a secret.
func TestRequestBodyForLogLeavesOutBodiesItCantRedact(t *testing.T) {
	head := []byte(`{"password":"` + strings.Repeat("x", httputil.BodyTruncationLimit) + `"}`)
	head = head[:httputil.BodyTruncationLimit+1] // all logRequestBody reads of a longer body
	require.Equal(t, "[not logged: too long to redact]", requestBodyForLog(head))
	require.Equal(t, "[not logged: not valid JSON, so it can't be redacted]", requestBodyForLog([]byte(`password=x`)))
	require.Equal(t, `{"password":"[REDACTED]"}`, requestBodyForLog([]byte(`{"password":"x"}`)))
}
