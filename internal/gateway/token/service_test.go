package token

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func setupTokenServiceTest(t *testing.T) (*db.User, *Service) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("test@example.com", "Test User", []string{"deployer"})
	require.NoError(t, err)
	service := NewService(database)
	return user, service
}

func TestGenerateAPIToken(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	req := &APITokenRequest{
		Name:        "CI/CD Token",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
		ExpiresAt:   DefaultTokenExpiry(),
	}

	resp, err := service.GenerateAPIToken(req)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Token)
	assert.True(t, len(resp.Token) > 10)
	assert.True(t, resp.Token[:4] == "rgw_") // Check prefix
	assert.Equal(t, req.Name, resp.APIToken.Name)
	assert.Equal(t, req.UserID, resp.APIToken.UserID)
	assert.Equal(t, req.Permissions, resp.APIToken.Permissions)

	// Validate the token
	validated, err := service.ValidateAPIToken(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, resp.APIToken.ID, validated.ID)
	assert.Equal(t, req.Name, validated.Name)
}

func TestValidateInvalidToken(t *testing.T) {
	_, service := setupTokenServiceTest(t)

	// Try invalid token
	_, err := service.ValidateAPIToken("invalid_token")
	assert.Error(t, err)

	// Try valid format but non-existent token
	_, err = service.ValidateAPIToken("rgw_fakefakefakefakefake")
	assert.Error(t, err)
}

func TestHasPermission(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	// Create token with specific permissions
	req := &APITokenRequest{
		Name:        "Limited Token",
		UserID:      user.ID,
		Permissions: []string{"convox:app:list", "convox:build:create"},
		ExpiresAt:   DefaultTokenExpiry(),
	}

	resp, err := service.GenerateAPIToken(req)
	require.NoError(t, err)

	// Test permissions
	assert.True(t, service.HasPermission(resp.APIToken, "app", "list"))
	assert.True(t, service.HasPermission(resp.APIToken, "build", "create"))
	assert.False(t, service.HasPermission(resp.APIToken, "env", "set"))

	// Test wildcard token
	wildcardReq := &APITokenRequest{
		Name:        "Admin Token",
		UserID:      user.ID,
		Permissions: []string{"convox:*:*"},
		ExpiresAt:   DefaultTokenExpiry(),
	}

	wildcardResp, err := service.GenerateAPIToken(wildcardReq)
	require.NoError(t, err)

	assert.True(t, service.HasPermission(wildcardResp.APIToken, "app", "list"))
	assert.True(t, service.HasPermission(wildcardResp.APIToken, "env", "set"))
	assert.True(t, service.HasPermission(wildcardResp.APIToken, "anything", "delete"))
}

func TestListAndDeleteTokens(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	// Create a couple tokens
	req1 := &APITokenRequest{
		Name:        "Token 1",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
		ExpiresAt:   DefaultTokenExpiry(),
	}
	req2 := &APITokenRequest{
		Name:        "Token 2",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
		ExpiresAt:   DefaultTokenExpiry(),
	}

	resp1, err := service.GenerateAPIToken(req1)
	require.NoError(t, err)
	resp2, err := service.GenerateAPIToken(req2)
	require.NoError(t, err)

	// List tokens
	tokens, err := service.ListTokensForUser(user.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tokens), 2) // At least the 2 we just created

	// Delete one token
	err = service.DeleteToken(resp1.APIToken.ID)
	assert.NoError(t, err)

	// Verify it's gone
	_, err = service.ValidateAPIToken(resp1.Token)
	assert.Error(t, err)

	// But the other should still work
	validated, err := service.ValidateAPIToken(resp2.Token)
	assert.NoError(t, err)
	assert.Equal(t, resp2.APIToken.ID, validated.ID)
}

func TestExpiredToken(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	// Create token that expires immediately
	expiredTime := time.Now().Add(-1 * time.Hour)
	req := &APITokenRequest{
		Name:        "Expired Token",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
		ExpiresAt:   &expiredTime,
	}

	resp, err := service.GenerateAPIToken(req)
	require.NoError(t, err)

	// Should fail validation due to expiry
	_, err = service.ValidateAPIToken(resp.Token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestRenameToken(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	// Create a token to rename
	req := &APITokenRequest{
		Name:        "Old Name",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
		ExpiresAt:   DefaultTokenExpiry(),
	}

	resp, err := service.GenerateAPIToken(req)
	require.NoError(t, err)

	// Rename the token
	newName := "New Name"
	err = service.UpdateTokenName(resp.APIToken.ID, newName)
	assert.NoError(t, err)

	// Reject empty name updates
	err = service.UpdateTokenName(resp.APIToken.ID, "   ")
	assert.ErrorIs(t, err, ErrAPITokenNameRequired)

	// Verify via list
	tokens, err := service.ListTokensForUser(user.ID)
	require.NoError(t, err)
	var found *db.APIToken
	for _, tk := range tokens {
		if tk.ID == resp.APIToken.ID {
			found = tk
			break
		}
	}
	require.NotNil(t, found, "renamed token not found in list")
	assert.Equal(t, newName, found.Name)
}

func TestDuplicateTokenNameRejected(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	name := "Deploy Token"
	req := &APITokenRequest{
		Name:        name,
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
	}
	_, err := service.GenerateAPIToken(req)
	require.NoError(t, err)

	_, err = service.GenerateAPIToken(req)
	assert.ErrorIs(t, err, ErrAPITokenNameExists)
}

func TestEmptyTokenNameRejected(t *testing.T) {
	user, service := setupTokenServiceTest(t)

	req := &APITokenRequest{
		Name:        "   ",
		UserID:      user.ID,
		Permissions: DefaultCICDPermissions(),
	}
	_, err := service.GenerateAPIToken(req)
	assert.ErrorIs(t, err, ErrAPITokenNameRequired)
}
