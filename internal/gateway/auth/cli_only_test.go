package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/golang-jwt/jwt/v5"
)

// setupTestDatabase creates a temporary test database for isolation
func setupTestDatabase(t *testing.T) *db.Database {
	t.Helper()
	// This creates a unique database like cg_test_<timestamp>
	// and automatically drops it when the test completes
	return dbtest.NewDatabase(t)
}

// MockTokenService for testing
type MockTokenService struct {
	tokens map[string]*db.APIToken
}

func (m *MockTokenService) ValidateAPIToken(token string) (*db.APIToken, error) {
	if apiToken, ok := m.tokens[token]; ok {
		if apiToken.ExpiresAt.After(time.Now()) {
			return apiToken, nil
		}
		return nil, fmt.Errorf("token expired")
	}
	return nil, fmt.Errorf("invalid token")
}

func (m *MockTokenService) CreateAPIToken(userID uint, name string, permissions []string, expiresAt time.Time) (*db.APIToken, string, error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (m *MockTokenService) GetAPITokensByUser(userID uint) ([]*db.APIToken, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockTokenService) DeleteAPIToken(tokenID string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockTokenService) GetAPIToken(tokenID string) (*db.APIToken, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockTokenService) UpdateAPIToken(tokenID string, name string, permissions []string) error {
	return fmt.Errorf("not implemented")
}

// Helper function to create Basic auth header
func basicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

func TestCLIOnlyMiddleware(t *testing.T) {
	// Setup
	database := setupTestDatabase(t)
	defer database.Close() //nolint:errcheck // test cleanup

	// Create a test user
	_, err := database.CreateUser("test@example.com", "Test User", []string{"admin"})
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create JWT manager and auth service
	jwtSecret := "test-secret"
	jwtManager := NewJWTManager(jwtSecret, 24*time.Hour)
	tokenService := token.NewService(database)
	sessionManager := NewSessionManager(database, jwtSecret, 24*time.Hour)
	authService := NewAuthService(jwtManager, tokenService, database, sessionManager)

	// Generate a valid JWT token
	validToken, _, err := jwtManager.GenerateToken("test@example.com", "Test User")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

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

	// Create a test handler that just returns 200 OK
	tt := t
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			tt.Fatalf("failed to write response: %v", err)
		}
	})

	// Wrap with CLIOnlyMiddleware
	handler := authService.CLIOnlyMiddleware(testHandler)

	tests := []struct {
		name           string
		authHeader     string
		cookie         *http.Cookie
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Bearer token in header - allowed",
			authHeader:     "Bearer " + validToken,
			cookie:         nil,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Basic auth with JWT - allowed",
			authHeader:     basicAuthHeader("convox", validToken),
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
			authHeader:     "Bearer " + validToken,
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
			authHeader:     "Custom " + validToken,
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "invalid authorization type for CLI access",
		},
		{
			name:           "Invalid Bearer token - blocked",
			authHeader:     "Bearer invalid-token",
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "invalid JWT",
		},
		{
			name:           "Expired token - blocked",
			authHeader:     "Bearer " + generateExpiredToken(jwtSecret),
			cookie:         nil,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "invalid JWT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/apps", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			body := rr.Body.String()
			if tt.expectedStatus == http.StatusOK {
				if body != tt.expectedBody {
					t.Errorf("expected body %q, got %q", tt.expectedBody, body)
				}
			} else {
				// For error responses, just check that the error message is present
				if !contains(body, tt.expectedBody) {
					t.Errorf("expected error containing %q, got %q", tt.expectedBody, body)
				}
			}
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
		UserID:      int64(testUser.ID),
		Permissions: []string{"*"},
		ExpiresAt:   &expiresAt,
	}
	tokenResp, err := tokenService.GenerateAPIToken(tokenReq)
	if err != nil {
		t.Fatalf("Failed to create API token: %v", err)
	}
	rawToken := tokenResp.Token
	apiToken := tokenResp.APIToken

	jwtManager := NewJWTManager("test-secret", 24*time.Hour)
	authService := NewAuthService(jwtManager, tokenService, database, nil)

	// Create a test handler
	tt := t
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestCLIOnlyMiddlewarePreventsBrowserCSRF(t *testing.T) {
	// This test ensures that even with a valid cookie, state-changing operations
	// cannot be performed from a browser context (CSRF protection)

	database := setupTestDatabase(t)
	defer database.Close() //nolint:errcheck // test cleanup

	// Create a test user
	_, err := database.CreateUser("test@example.com", "Test User", []string{"deployer"})
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	jwtManager := NewJWTManager("test-secret", 24*time.Hour)
	tokenService := token.NewService(database)
	sessionManager := NewSessionManager(database, "test-secret", 24*time.Hour)
	authService := NewAuthService(jwtManager, tokenService, database, sessionManager)

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

	// Create a handler that simulates a dangerous operation
	ttDanger := t
	dangerousHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should never be reached from a browser
		ttDanger.Error("Dangerous operation was allowed with cookie auth!")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("DANGER: Operation performed")); err != nil {
			ttDanger.Fatalf("failed to write response: %v", err)
		}
	})

	handler := authService.CLIOnlyMiddleware(dangerousHandler)

	// Simulate various browser-based CSRF attack vectors
	csrfTests := []struct {
		name   string
		method string
		path   string
		cookie *http.Cookie
		origin string
	}{
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

	for _, tt := range csrfTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.AddCookie(tt.cookie)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			// Add typical browser headers
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
			req.Header.Set("Accept", "application/json")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// All these requests should be blocked
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("CSRF attack not blocked! Expected 401, got %d", rr.Code)
			}

			// Verify the error message
			body := rr.Body.String()
			if !contains(body, "browser session cookies are not permitted") {
				t.Errorf("Expected browser-session block message, got: %s", body)
			}
		})
	}
}

// Helper function to generate expired token
func generateExpiredToken(secret string) string {
	claims := &Claims{
		Email: "test@example.com",
		Name:  "Test User",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(secret))
	return tokenString
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
