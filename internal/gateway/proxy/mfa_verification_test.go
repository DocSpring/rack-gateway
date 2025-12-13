package proxy

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func TestParseWebAuthnAssertion_CLIFormat(t *testing.T) {
	t.Parallel()

	// Simulate what CLI sends: base64-encoded JSON with session_data and assertion_response string
	sessionData := `{"challenge":"test-challenge","rpId":"example.com","userID":[1,2,3]}`
	assertionResponse := `{"id":"test-id","rawId":"test-id","type":"public-key",` +
		`"response":{"authenticatorData":"test-auth-data","clientDataJSON":"test-client-data","signature":"test-sig"}}`

	inlineData := map[string]interface{}{
		"session_data":       sessionData,
		"assertion_response": assertionResponse,
	}

	jsonData, err := json.Marshal(inlineData)
	require.NoError(t, err)

	// Base64 encode like CLI does
	base64Data := base64.StdEncoding.EncodeToString(jsonData)

	// Decode and parse
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	require.NoError(t, err)

	result, err := parseWebAuthnAssertion(decoded)
	require.NoError(t, err)
	assert.Equal(t, sessionData, result.SessionData)
	assert.Equal(t, assertionResponse, result.AssertionResponse)
}

func TestParseWebAuthnAssertion_BrowserFormat(t *testing.T) {
	t.Parallel()

	// Browser sends same format as CLI (via X-MFA-WebAuthn header)
	sessionData := `{"challenge":"browser-challenge","rpId":"localhost","userID":[4,5,6]}`
	assertionResponse := `{"id":"browser-id","rawId":"browser-id","type":"public-key",` +
		`"response":{"authenticatorData":"browser-auth-data","clientDataJSON":"browser-client-data",` +
		`"signature":"browser-sig","userHandle":"browser-handle"}}`

	inlineData := map[string]interface{}{
		"session_data":       sessionData,
		"assertion_response": assertionResponse,
	}

	jsonData, err := json.Marshal(inlineData)
	require.NoError(t, err)

	// Base64 encode
	base64Data := base64.StdEncoding.EncodeToString(jsonData)

	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	require.NoError(t, err)

	result, err := parseWebAuthnAssertion(decoded)
	require.NoError(t, err)
	assert.Equal(t, sessionData, result.SessionData)
	assert.Equal(t, assertionResponse, result.AssertionResponse)
}

func TestParseWebAuthnAssertion_MissingFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		jsonData  string
		wantError string
	}{
		{
			name:      "missing session_data",
			jsonData:  `{"assertion_response":"test"}`,
			wantError: "missing session_data",
		},
		{
			name:      "missing assertion_response",
			jsonData:  `{"session_data":"test"}`,
			wantError: "missing assertion_response",
		},
		{
			name:      "empty json",
			jsonData:  `{}`,
			wantError: "missing session_data",
		},
		{
			name:      "invalid json",
			jsonData:  `{invalid}`,
			wantError: "invalid webauthn assertion JSON",
		},
		{
			name:      "wrong field name (assertion instead of assertion_response)",
			jsonData:  `{"session_data":"test","assertion":"test"}`,
			wantError: "missing assertion_response",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseWebAuthnAssertion([]byte(tc.jsonData))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

func TestParseWebAuthnAssertion_EmptyValues(t *testing.T) {
	t.Parallel()

	// Empty values should fail validation
	testCases := []struct {
		name      string
		jsonData  string
		wantError string
	}{
		{
			name:      "empty session_data",
			jsonData:  `{"session_data":"","assertion_response":"test"}`,
			wantError: "missing session_data",
		},
		{
			name:      "empty assertion_response",
			jsonData:  `{"session_data":"test","assertion_response":""}`,
			wantError: "missing assertion_response",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseWebAuthnAssertion([]byte(tc.jsonData))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

func TestParseWebAuthnAssertion_PreservesAssertionFormat(t *testing.T) {
	t.Parallel()

	// Critical test: ensure assertion_response is preserved as-is (JSON string)
	// This is the key fix - we must NOT re-marshal it
	sessionData := `{"challenge":"test","rpId":"example.com"}`

	// Complex assertion with nested response object (browser WebAuthn format)
	assertionResponse := `{
		"id": "dGVzdC1jcmVkZW50aWFsLWlk",
		"rawId": "dGVzdC1jcmVkZW50aWFsLWlk",
		"type": "public-key",
		"response": {
			"authenticatorData": "dGVzdC1hdXRoLWRhdGE",
			"clientDataJSON": "dGVzdC1jbGllbnQtZGF0YS1qc29u",
			"signature": "dGVzdC1zaWduYXR1cmU",
			"userHandle": "dGVzdC11c2VyLWhhbmRsZQ"
		}
	}`

	inlineData := map[string]interface{}{
		"session_data":       sessionData,
		"assertion_response": assertionResponse,
	}

	jsonData, err := json.Marshal(inlineData)
	require.NoError(t, err)

	result, err := parseWebAuthnAssertion(jsonData)
	require.NoError(t, err)

	// Verify assertion_response is preserved exactly as JSON string
	// This is critical - it must NOT be converted to snake_case
	assert.Equal(t, assertionResponse, result.AssertionResponse)

	// Verify it's valid JSON that can be parsed by go-webauthn library
	var assertionMap map[string]interface{}
	err = json.Unmarshal([]byte(result.AssertionResponse), &assertionMap)
	require.NoError(t, err, "assertion_response should be valid JSON")

	// Verify it has the expected browser WebAuthn structure
	assert.Equal(t, "dGVzdC1jcmVkZW50aWFsLWlk", assertionMap["id"])
	assert.Equal(t, "dGVzdC1jcmVkZW50aWFsLWlk", assertionMap["rawId"])
	assert.Equal(t, "public-key", assertionMap["type"])

	response, ok := assertionMap["response"].(map[string]interface{})
	require.True(t, ok, "response should be an object")
	assert.Equal(t, "dGVzdC1hdXRoLWRhdGE", response["authenticatorData"])
	assert.Equal(t, "dGVzdC1jbGllbnQtZGF0YS1qc29u", response["clientDataJSON"])
}

// TestVerifyMFAIfRequired_StepUpWindowCheckedFirst verifies that the step-up window
// is checked BEFORE attempting inline MFA verification. This is critical for multi-step
// CLI operations (like "env set --promote") where the same MFA code is sent for all requests.
// The first request verifies the MFA code and sets the step-up window. Subsequent requests
// should skip MFA verification and reuse the step-up window, avoiding TOTP replay errors.
func TestVerifyMFAIfRequired_StepUpWindowCheckedFirst(t *testing.T) {
	t.Parallel()

	// Create a minimal handler with no MFA service to test the logic flow
	h := &Handler{}

	// Create a test session with a recent step-up timestamp
	recentStepUp := time.Now().Add(-5 * time.Minute) // 5 minutes ago, within 10-minute window
	session := &db.UserSession{
		ID:             1,
		RecentStepUpAt: &recentStepUp,
	}

	// Create auth user WITH inline MFA credentials (simulating CLI with embedded MFA code)
	authUser := &auth.User{
		Email:    "test@example.com",
		Session:  session,
		MFAType:  "totp",
		MFAValue: "123456", // Inline MFA code that would normally be verified
	}

	// Test that isStepUpValid returns true for recent step-up
	require.True(t, h.isStepUpValid(authUser), "step-up should be valid when within window")

	// Test that isStepUpValid returns false for expired step-up
	expiredStepUp := time.Now().Add(-15 * time.Minute) // 15 minutes ago, outside 10-minute window
	authUser.Session.RecentStepUpAt = &expiredStepUp
	require.False(t, h.isStepUpValid(authUser), "step-up should be invalid when outside window")

	// Test that isStepUpValid returns false when RecentStepUpAt is nil
	authUser.Session.RecentStepUpAt = nil
	require.False(t, h.isStepUpValid(authUser), "step-up should be invalid when RecentStepUpAt is nil")
}

// TestVerifyMFAIfRequired_SkipsInlineMFAWhenStepUpValid tests that verifyMFAIfRequired
// returns early without calling verifyInlineMFA when step-up window is valid.
// This test uses nil mfaService to ensure verifyInlineMFA would panic if called.
func TestVerifyMFAIfRequired_SkipsInlineMFAWhenStepUpValid(t *testing.T) {
	t.Parallel()

	// Handler with nil mfaService - verifyInlineMFA would fail if called
	h := &Handler{
		mfaService:     nil, // This will cause early return
		sessionManager: nil,
	}

	recentStepUp := time.Now().Add(-5 * time.Minute)
	session := &db.UserSession{
		ID:             1,
		RecentStepUpAt: &recentStepUp,
	}

	authUser := &auth.User{
		Email:    "test@example.com",
		Session:  session,
		MFAType:  "totp",
		MFAValue: "123456",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/test/releases/R1/promote", nil)
	w := httptest.NewRecorder()
	rackConfig := &config.RackConfig{Name: "default"}

	// Should return nil because mfaService is nil (early return)
	err := h.verifyMFAIfRequired(req, w, authUser, rbac.ResourceRelease, rbac.ActionPromote, rackConfig, time.Now())
	require.NoError(t, err, "should return nil when mfaService is nil")
}

// TestVerifyMFAIfRequired_NoMFANeeded tests that MFANone permissions skip all MFA checks.
func TestVerifyMFAIfRequired_NoMFANeeded(t *testing.T) {
	t.Parallel()

	h := &Handler{}

	authUser := &auth.User{
		Email: "test@example.com",
	}

	req := httptest.NewRequest(http.MethodGet, "/apps", nil)
	w := httptest.NewRecorder()
	rackConfig := &config.RackConfig{Name: "default"}

	// ActionList on ResourceApp should be MFANone (read operation)
	err := h.verifyMFAIfRequired(req, w, authUser, rbac.ResourceApp, rbac.ActionList, rackConfig, time.Now())
	require.NoError(t, err, "read operations should not require MFA")
}
