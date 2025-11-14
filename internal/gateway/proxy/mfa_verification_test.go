package proxy

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
