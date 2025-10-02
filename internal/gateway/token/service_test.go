package token

import (
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenService(t *testing.T) {
	database := dbtest.NewDatabase(t)

	// Create a test user
	user, err := database.CreateUser("test@example.com", "Test User", []string{"deployer"})
	require.NoError(t, err)

	service := NewService(database)

	t.Run("GenerateAPIToken", func(t *testing.T) {
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
		assert.True(t, resp.Token[:4] == "cgw_") // Check prefix
		assert.Equal(t, req.Name, resp.APIToken.Name)
		assert.Equal(t, req.UserID, resp.APIToken.UserID)
		assert.Equal(t, req.Permissions, resp.APIToken.Permissions)

		// Validate the token
		validated, err := service.ValidateAPIToken(resp.Token)
		require.NoError(t, err)
		assert.Equal(t, resp.APIToken.ID, validated.ID)
		assert.Equal(t, req.Name, validated.Name)
	})

	t.Run("ValidateInvalidToken", func(t *testing.T) {
		// Try invalid token
		_, err := service.ValidateAPIToken("invalid_token")
		assert.Error(t, err)

		// Try valid format but non-existent token
		_, err = service.ValidateAPIToken("cgw_fakefakefakefakefake")
		assert.Error(t, err)
	})

	t.Run("HasPermission", func(t *testing.T) {
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
	})

	t.Run("ListAndDeleteTokens", func(t *testing.T) {
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
	})

	t.Run("ExpiredToken", func(t *testing.T) {
		// Create token that expires immediately
		req := &APITokenRequest{
			Name:        "Expired Token",
			UserID:      user.ID,
			Permissions: DefaultCICDPermissions(),
			ExpiresAt:   func() *time.Time { t := time.Now().Add(-1 * time.Hour); return &t }(), // 1 hour ago
		}

		resp, err := service.GenerateAPIToken(req)
		require.NoError(t, err)

		// Should fail validation due to expiry
		_, err = service.ValidateAPIToken(resp.Token)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expired")
	})

	// New: rename (update) token name
	t.Run("RenameToken", func(t *testing.T) {
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
	})

	t.Run("DuplicateTokenNameRejected", func(t *testing.T) {
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
	})

	t.Run("EmptyTokenNameRejected", func(t *testing.T) {
		req := &APITokenRequest{
			Name:        "   ",
			UserID:      user.ID,
			Permissions: DefaultCICDPermissions(),
		}
		_, err := service.GenerateAPIToken(req)
		assert.ErrorIs(t, err, ErrAPITokenNameRequired)
	})
}
