//go:build integration
// +build integration

package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestOAuthEndpoint_UsesCustomBaseURL(t *testing.T) {
	// Create OAuth handler with mock base URL
	jwtManager := NewJWTManager("test-secret", time.Hour)
	handler := NewOAuthHandler(
		"mock-client-id",
		"mock-client-secret",
		"http://localhost:8447",
		"company.com",
		"http://mock-oauth:3001", // This should make it use mock endpoints
		jwtManager,
	)

	// Set up HTTP handler
	r := chi.NewRouter()
	r.Post("/login/start", func(w http.ResponseWriter, r *http.Request) {
		resp, err := handler.StartLogin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Create test server
	server := httptest.NewServer(r)
	defer server.Close()

	// Make request to login start endpoint
	reqBody := map[string]interface{}{
		"code_challenge": "test-challenge",
		"state":          "test-state",
		"rack":           "default",
		"redirect_uri":   "http://localhost:5173/auth/callback",
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL+"/login/start", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	authURL, ok := response["auth_url"].(string)
	if !ok {
		t.Fatalf("Missing or invalid auth_url in response")
	}

	// This is the critical test: should NOT redirect to Google
	if strings.Contains(authURL, "accounts.google.com") {
		t.Errorf("FAIL: OAuth is redirecting to Google instead of mock server. AuthURL: %s", authURL)
		t.Errorf("This means the GOOGLE_OAUTH_BASE_URL environment variable is not being used correctly")
	}

	// Should redirect to mock OAuth server
	if !strings.Contains(authURL, "mock-oauth:3001") {
		t.Errorf("Expected mock OAuth URL containing 'mock-oauth:3001', got: %s", authURL)
	}

	t.Logf("SUCCESS: Auth URL correctly uses mock server: %s", authURL)
}
