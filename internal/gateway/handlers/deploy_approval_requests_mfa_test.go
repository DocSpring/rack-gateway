package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func TestApproveDeployApprovalRequest_RequiresMFACode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create admin user with TOTP MFA enrolled
	admin, err := database.CreateUser("admin@test.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create TOTP MFA method for admin
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)

	err = database.ConfirmMFAMethod(method.ID, time.Now())
	require.NoError(t, err)

	// Create API token for deploy requests
	tokenHash := "test-token-hash-12345"
	token, err := database.CreateAPIToken(tokenHash, "test-token", admin.ID, []string{"deployer"}, nil, nil)
	require.NoError(t, err)

	// Create a pending deploy approval request
	req, err := database.CreateDeployApprovalRequest(
		"Test deploy",
		"test-app",
		"abc123",
		"main",
		"https://example.com/pipeline",
		"github",
		nil,       // ciMetadata
		admin.ID,  // createdByUserID
		nil,       // createdByAPITokenID
		token.ID,  // targetAPITokenID
		&admin.ID, // targetUserID
	)
	require.NoError(t, err)
	require.Equal(t, "pending", req.Status)

	// Create handler and MFA service
	rbacManager := newAllowAllRBAC(admin)
	auditLogger := audit.NewLogger(database)
	handler := &AdminHandler{
		rbac:        rbacManager,
		database:    database,
		auditLogger: auditLogger,
		config:      &config.Config{},
	}

	// Create session manager and MFA service
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)

	// Get MFA settings
	mfaSettings, err := database.GetMFASettings()
	require.NoError(t, err)

	t.Run("denies approval without MFA code", func(t *testing.T) {
		// Create a session for the admin user with MFA verified
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)
		require.NotNil(t, session)

		// Set MFA verified but NO MFA code in request
		now := time.Now()
		session.MFAVerifiedAt = &now

		router := gin.New()
		router.POST("/admin/deploy-approval-requests/:id/approve", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				Session:    session,
				// NO MFAType or MFAValue - this is the key difference
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Set("user_name", admin.Name)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.ApproveDeployApprovalRequest)

		// Make request WITHOUT MFA code
		reqBody := UpdateDeployApprovalRequestStatusRequest{}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/deploy-approval-requests/"+req.PublicID+"/approve", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied because no MFA code provided
		require.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("allows approval with valid TOTP code", func(t *testing.T) {
		// Create a fresh deploy approval request
		req2, err := database.CreateDeployApprovalRequest(
			"Test deploy 2",
			"test-app-2",
			"def456",
			"main",
			"https://example.com/pipeline",
			"github",
			nil,
			admin.ID,
			nil,
			token.ID,
			&admin.ID,
		)
		require.NoError(t, err)

		// Create a fresh session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)
		require.NotNil(t, session)

		// Set MFA verified
		now := time.Now()
		session.MFAVerifiedAt = &now

		// Generate a valid TOTP code
		code, err := totp.GenerateCode(key.Secret(), time.Now())
		require.NoError(t, err)

		router := gin.New()
		router.POST("/admin/deploy-approval-requests/:id/approve", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				Session:    session,
				MFAType:    "totp", // Provide MFA type
				MFAValue:   code,   // Provide valid TOTP code
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Set("user_name", admin.Name)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.ApproveDeployApprovalRequest)

		// Make request WITH valid MFA code
		reqBody := UpdateDeployApprovalRequestStatusRequest{}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/deploy-approval-requests/"+req2.PublicID+"/approve", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should succeed
		require.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "approved", response["status"])
	})

	t.Run("denies approval with invalid TOTP code", func(t *testing.T) {
		// Create a fresh session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)
		require.NotNil(t, session)

		// Set MFA verified
		now := time.Now()
		session.MFAVerifiedAt = &now

		router := gin.New()
		router.POST("/admin/deploy-approval-requests/:id/approve", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				Session:    session,
				MFAType:    "totp",
				MFAValue:   "000000", // Invalid TOTP code
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Set("user_name", admin.Name)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.ApproveDeployApprovalRequest)

		// Make request WITH invalid MFA code
		reqBody := UpdateDeployApprovalRequestStatusRequest{}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/deploy-approval-requests/"+req.PublicID+"/approve", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied due to invalid code
		require.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_verification_failed", response["error"])
	})

	t.Run("denies API tokens from using MFA-protected operations", func(t *testing.T) {
		router := gin.New()
		router.POST("/admin/deploy-approval-requests/:id/approve", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:       admin.Email,
				Name:        admin.Name,
				Permissions: []string{"convox:apps:*"},
				IsAPIToken:  true, // This is an API token
				TokenID:     &token.ID,
				TokenName:   "test-token",
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Set("user_name", admin.Name)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.ApproveDeployApprovalRequest)

		// Make request as API token
		reqBody := UpdateDeployApprovalRequestStatusRequest{}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/deploy-approval-requests/"+req.PublicID+"/approve", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied - API tokens cannot bypass MFA
		require.Equal(t, http.StatusForbidden, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "api_token_not_allowed", response["error"])
	})
}

func TestCreateAPIToken_AlwaysRequiresMFACode(t *testing.T) {
	// This test documents that API token creation correctly uses RequireMFA
	// which always requires fresh MFA code with every request

	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create admin user with TOTP MFA enrolled
	admin, err := database.CreateUser("admin@test.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	// Create TOTP MFA method for admin
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Test",
		AccountName: admin.Email,
		Period:      30,
		Digits:      otp.DigitsSix,
	})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(admin.ID, "totp", "Authenticator", key.Secret(), nil, nil, nil, nil)
	require.NoError(t, err)

	err = database.ConfirmMFAMethod(method.ID, time.Now())
	require.NoError(t, err)

	// Create handler and MFA service
	rbacManager := newAllowAllRBAC(admin)
	auditLogger := audit.NewLogger(database)
	tokenService := token.NewService(database)
	handler := &AdminHandler{
		rbac:         rbacManager,
		database:     database,
		auditLogger:  auditLogger,
		config:       &config.Config{},
		tokenService: tokenService,
	}

	// Create session manager and MFA service
	sessionManager := auth.NewSessionManager(database, "test-secret", time.Hour)
	pepper := []byte("test-pepper")
	mfaService, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, pepper, "", "", "", "", nil)
	require.NoError(t, err)

	// Get MFA settings
	mfaSettings, err := database.GetMFASettings()
	require.NoError(t, err)

	t.Run("denies token creation without MFA code", func(t *testing.T) {
		// Create a session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		// Set MFA verified but NO MFA code in request
		now := time.Now()
		session.MFAVerifiedAt = &now

		router := gin.New()
		router.POST("/admin/api-tokens", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				Session:    session,
				// NO MFAType or MFAValue
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.CreateAPIToken)

		reqBody := map[string]interface{}{
			"name":        "new-token",
			"permissions": []string{"convox:apps:read"},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/api-tokens", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should be denied
		require.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "mfa_required", response["error"])
	})

	t.Run("allows token creation with valid MFA code", func(t *testing.T) {
		// Create a fresh session for the admin user
		_, session, err := sessionManager.CreateSession(admin, auth.SessionMetadata{Channel: "web"})
		require.NoError(t, err)

		// Set MFA verified
		now := time.Now()
		session.MFAVerifiedAt = &now

		// Generate a valid TOTP code
		code, err := totp.GenerateCode(key.Secret(), time.Now())
		require.NoError(t, err)

		router := gin.New()
		router.POST("/admin/api-tokens", func(c *gin.Context) {
			authUser := &auth.AuthUser{
				Email:      admin.Email,
				Name:       admin.Name,
				Roles:      admin.Roles,
				IsAPIToken: false,
				Session:    session,
				MFAType:    "totp",
				MFAValue:   code,
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
			c.Set("user_email", admin.Email)
			c.Next()
		}, middleware.RequireMFA(mfaService, database, mfaSettings), handler.CreateAPIToken)

		reqBody := map[string]interface{}{
			"name":        "new-token",
			"permissions": []string{"convox:apps:read"},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		w := httptest.NewRecorder()
		httpReq, _ := http.NewRequest("POST", "/admin/api-tokens", bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, httpReq)

		// Should succeed
		require.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotEmpty(t, response["token"])
	})
}
