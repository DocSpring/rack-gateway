package auth

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOAuthHandler_UsesCustomBaseURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OAuth network-dependent test in short mode")
	}

	baseURL := os.Getenv("MOCK_OAUTH_BASE_URL")
	if baseURL == "" {
		t.Skip("MOCK_OAUTH_BASE_URL not set; skipping network-dependent test")
	}

	// quick resolvability check to avoid hard failures on DNS
	host := strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
	if h, _, ok := strings.Cut(host, "/"); ok {
		host = h
	}
	if _, err := net.LookupHost(host); err != nil {
		t.Skipf("issuer host %q not resolvable: %v", host, err)
	}

	jwtManager := NewJWTManager("test-secret", time.Hour)

	// Test with custom base URL (should use mock OAuth endpoints)
	handler, err := NewOAuthHandler(
		"mock-client-id",
		"mock-client-secret",
		"http://localhost:8447",
		"company.com",
		baseURL, // Custom issuer URL
		jwtManager,
	)
	if err != nil {
		t.Fatalf("Failed to create OAuth handler: %v", err)
	}

	response, err := handler.StartLogin()
	if err != nil {
		t.Fatalf("StartLogin failed: %v", err)
	}

	// Should use mock OAuth server, not Google
	if strings.Contains(response.AuthURL, "accounts.google.com") {
		t.Errorf("Expected mock OAuth URL, but got Google URL: %s", response.AuthURL)
	}

	if !strings.Contains(response.AuthURL, baseURL) {
		t.Errorf("Expected mock OAuth base URL %s in auth URL, got: %s", baseURL, response.AuthURL)
	}
}

func TestOAuthHandler_UsesGoogleByDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OAuth network-dependent test in short mode")
	}
	jwtManager := NewJWTManager("test-secret", time.Hour)

	// Test without custom base URL (should use Google OAuth endpoints)
	handler, err := NewOAuthHandler(
		"real-client-id",
		"real-client-secret",
		"http://localhost:8447",
		"company.com",
		"https://accounts.google.com", // Google issuer URL
		jwtManager,
	)
	if err != nil {
		t.Fatalf("Failed to create OAuth handler: %v", err)
	}

	response, err := handler.StartLogin()
	if err != nil {
		t.Fatalf("StartLogin failed: %v", err)
	}

	// Should use Google OAuth server
	if !strings.Contains(response.AuthURL, "accounts.google.com") {
		t.Errorf("Expected Google OAuth URL, but got: %s", response.AuthURL)
	}
}
