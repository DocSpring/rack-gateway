package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

// setupTestDatabase creates a temporary test database for isolation
func setupTestDatabase(t *testing.T) *db.Database {
	t.Helper()
	// This creates a unique database like rgw_test_<timestamp>
	// and automatically drops it when the test completes
	return dbtest.NewDatabase(t)
}

// MockTokenService for testing
type MockTokenService struct {
	tokens map[string]*db.APIToken
}

func (m *MockTokenService) ValidateAPIToken(tokenValue string) (*db.APIToken, error) {
	if apiToken, ok := m.tokens[tokenValue]; ok {
		if apiToken.ExpiresAt.After(time.Now()) {
			return apiToken, nil
		}
		return nil, fmt.Errorf("token expired")
	}
	return nil, fmt.Errorf("invalid token")
}

func (m *MockTokenService) CreateAPIToken(
	_ uint,
	_ string,
	_ []string,
	_ time.Time,
) (*db.APIToken, string, error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (m *MockTokenService) GetAPITokensByUser(_ uint) ([]*db.APIToken, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockTokenService) DeleteAPIToken(_ string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockTokenService) GetAPIToken(_ string) (*db.APIToken, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockTokenService) UpdateAPIToken(_ string, _ string, _ []string) error {
	return fmt.Errorf("not implemented")
}

// Helper function to create Basic auth header
func basicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

type cliTestCase struct {
	name           string
	authHeader     string
	cookie         *http.Cookie
	expectedStatus int
	expectedBody   string
}

func setupCLITestEnvironment(t *testing.T) (*db.Database, *Service, string) {
	t.Helper()
	database := setupTestDatabase(t)

	// Create a test user
	_, err := database.CreateUser("test@example.com", "Test User", []string{"admin"})
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create auth service
	tokenService := token.NewService(database)
	sessionManager := NewSessionManager(database, "test-secret", 24*time.Hour)
	authService := NewAuthService(tokenService, database, sessionManager)

	// Get user record and create session token
	userRecord, err := database.GetUser("test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if userRecord == nil {
		t.Fatalf("expected user record")
	}

	sessionToken, _, err := sessionManager.CreateSession(userRecord, SessionMetadata{Channel: "cli"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	return database, authService, sessionToken
}

func runCLITestCase(t *testing.T, handler http.Handler, tc cliTestCase) {
	t.Helper()
	req := httptest.NewRequest("GET", "/apps", nil)
	if tc.authHeader != "" {
		req.Header.Set("Authorization", tc.authHeader)
	}
	if tc.cookie != nil {
		req.AddCookie(tc.cookie)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != tc.expectedStatus {
		t.Errorf("expected status %d, got %d", tc.expectedStatus, rr.Code)
	}

	body := rr.Body.String()
	if tc.expectedStatus == http.StatusOK {
		if body != tc.expectedBody {
			t.Errorf("expected body %q, got %q", tc.expectedBody, body)
		}
	} else {
		if !contains(body, tc.expectedBody) {
			t.Errorf("expected error containing %q, got %q", tc.expectedBody, body)
		}
	}
}

func TestCLIOnlyMiddleware(t *testing.T) {
	database, authService, sessionToken := setupCLITestEnvironment(t)
	defer database.Close() //nolint:errcheck // test cleanup

	// Create a test handler that just returns 200 OK
	tt := t
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			tt.Fatalf("failed to write response: %v", err)
		}
	})

	handler := authService.CLIOnlyMiddleware(testHandler)

	tests := []cliTestCase{
		{
			name:           "Bearer token in header - allowed",
			authHeader:     "Bearer " + sessionToken,
			cookie:         nil,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Basic auth with session token - allowed",
			authHeader:     basicAuthHeader("convox", sessionToken),
			cookie:         nil,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Cookie only - blocked",
			authHeader:     "",
			cookie:         &http.Cookie{Name: "session_token", Value: sessionToken},
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "browser session cookies are not permitted for CLI routes",
		},
		{
			name:           "Both cookie and header - blocked (cookie indicates browser)",
			authHeader:     "Bearer " + sessionToken,
			cookie:         &http.Cookie{Name: "session_token", Value: sessionToken},
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "browser session cookies are not permitted for CLI routes",
		},
		{
			name:           "No auth at all - blocked",
			authHeader:     "",
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "CLI authentication required - provide Authorization header",
		},
		{
			name:           "Invalid auth type - blocked",
			authHeader:     "Custom " + sessionToken,
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "invalid authorization type for CLI access",
		},
		{
			name:           "Invalid Bearer token - blocked",
			authHeader:     "Bearer invalid-token",
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "authentication failed",
		},
		{
			name:           "Expired token - blocked",
			authHeader:     "Bearer expired-session-token",
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "authentication failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runCLITestCase(t, handler, tc)
		})
	}
}

func TestCLIOnlyMiddlewareWithAPIToken(t *testing.T) {
	// Setup
	database := setupTestDatabase(t)
	defer database.Close() //nolint:errcheck // test cleanup

	// Create a test user
	testUser, err := database.CreateUser("test@example.com", "Test User", []string{"admin"})
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create a real API token in the database
	tokenService := token.NewService(database)
	expiresAt := time.Now().Add(24 * time.Hour)
	tokenReq := &token.APITokenRequest{
		Name:        "Test Token",
		UserID:      testUser.ID,
		Permissions: []string{"*"},
		ExpiresAt:   &expiresAt,
	}
	tokenResp, err := tokenService.GenerateAPIToken(tokenReq)
	if err != nil {
		t.Fatalf("Failed to create API token: %v", err)
	}
	rawToken := tokenResp.Token
	apiToken := tokenResp.APIToken

	authService := NewAuthService(tokenService, database, nil)

	// Create a test handler
	tt := t
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			tt.Fatalf("failed to write response: %v", err)
		}
	})

	handler := authService.CLIOnlyMiddleware(testHandler)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "Valid API token",
			authHeader:     "Bearer " + rawToken,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid API token",
			authHeader:     "Bearer invalid-api-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "API token in Basic auth",
			authHeader:     basicAuthHeader("x", rawToken),
			expectedStatus: http.StatusOK,
		},
	}

	// Silence unused variable warning
	_ = apiToken

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/apps/myapp/scale", nil)
			req.Header.Set("Authorization", tt.authHeader)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

type csrfTestCase struct {
	name   string
	method string
	path   string
	cookie *http.Cookie
	origin string
}

func createCSRFTestCases(sessionToken string) []csrfTestCase {
	return []csrfTestCase{
		{
			name:   "POST to deploy endpoint",
			method: "POST",
			path:   "/apps/production-app/builds",
			cookie: &http.Cookie{Name: "session_token", Value: sessionToken},
			origin: "https://evil.com",
		},
		{
			name:   "DELETE to destroy resources",
			method: "DELETE",
			path:   "/apps/production-app",
			cookie: &http.Cookie{Name: "session_token", Value: sessionToken},
			origin: "https://attacker.com",
		},
		{
			name:   "PUT to scale up expensive resources",
			method: "PUT",
			path:   "/apps/production-app/processes/web",
			cookie: &http.Cookie{Name: "session_token", Value: sessionToken},
			origin: "https://malicious.site",
		},
		{
			name:   "POST to execute commands",
			method: "POST",
			path:   "/apps/production-app/processes/web/run",
			cookie: &http.Cookie{Name: "session_token", Value: sessionToken},
			origin: "",
		},
	}
}

func runCSRFTestCase(t *testing.T, handler http.Handler, tc csrfTestCase) {
	t.Helper()
	req := httptest.NewRequest(tc.method, tc.path, nil)
	req.AddCookie(tc.cookie)
	if tc.origin != "" {
		req.Header.Set("Origin", tc.origin)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.Header.Set("Accept", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("CSRF attack not blocked! Expected 401, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !contains(body, "browser session cookies are not permitted") {
		t.Errorf("Expected browser-session block message, got: %s", body)
	}
}

func TestCLIOnlyMiddlewarePreventsBrowserCSRF(t *testing.T) {
	// This test ensures that even with a valid cookie, state-changing operations
	// cannot be performed from a browser context (CSRF protection)

	database, authService, sessionToken := setupCLITestEnvironment(t)
	defer database.Close() //nolint:errcheck // test cleanup

	// Create a handler that simulates a dangerous operation
	ttDanger := t
	dangerousHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// This should never be reached from a browser
		ttDanger.Error("Dangerous operation was allowed with cookie auth!")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("DANGER: Operation performed")); err != nil {
			ttDanger.Fatalf("failed to write response: %v", err)
		}
	})

	handler := authService.CLIOnlyMiddleware(dangerousHandler)
	csrfTests := createCSRFTestCases(sessionToken)

	for _, tc := range csrfTests {
		t.Run(tc.name, func(t *testing.T) {
			runCSRFTestCase(t, handler, tc)
		})
	}
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr) && containsMiddle(s, substr))
}

func containsMiddle(s, substr string) bool {
	for i := 1; i < len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
