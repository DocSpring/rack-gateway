package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
)

// Service handles API token operations
type Service struct {
	db *db.Database
}

// APITokenRequest represents a request to create an API token
type APITokenRequest struct {
	Name        string     `json:"name"`
	UserID      int64      `json:"user_id"`
	Permissions []string   `json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

// APITokenResponse represents the response when creating an API token
type APITokenResponse struct {
	Token    string       `json:"token"`     // The actual token (only returned once)
	APIToken *db.APIToken `json:"api_token"` // Token metadata (without hash)
}

// NewService creates a new token service
func NewService(database *db.Database) *Service {
	return &Service{
		db: database,
	}
}

// GenerateAPIToken creates a new API token
func (s *Service) GenerateAPIToken(req *APITokenRequest) (*APITokenResponse, error) {
	// Generate a secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random token: %w", err)
	}

	// Create the token string with prefix
	token := "cgw_" + base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the token for storage
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Store in database
	apiToken, err := s.db.CreateAPIToken(tokenHash, req.Name, req.UserID, req.Permissions, req.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}

	return &APITokenResponse{
		Token:    token,
		APIToken: apiToken,
	}, nil
}

// ValidateAPIToken validates an API token and returns the associated user ID
func (s *Service) ValidateAPIToken(token string) (*db.APIToken, error) {
	// Check token format
	if !strings.HasPrefix(token, "cgw_") {
		return nil, fmt.Errorf("invalid token format")
	}

	// Hash the token to lookup
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Get token from database
	apiToken, err := s.db.GetAPITokenByHash(tokenHash)
	if err != nil {
		return nil, fmt.Errorf("failed to validate token: %w", err)
	}
	if apiToken == nil {
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token has expired
	if apiToken.ExpiresAt != nil && time.Now().After(*apiToken.ExpiresAt) {
		return nil, fmt.Errorf("token has expired")
	}

	// Update last used timestamp
	if err := s.db.UpdateAPITokenLastUsed(tokenHash); err != nil {
		// Log but don't fail - this is not critical
		fmt.Printf("Warning: Failed to update token last used: %v\n", err)
	}

	return apiToken, nil
}

// ListTokensForUser returns all API tokens for a user
func (s *Service) ListTokensForUser(userID int64) ([]*db.APIToken, error) {
	return s.db.ListAPITokensByUser(userID)
}

// DeleteToken removes an API token
func (s *Service) DeleteToken(tokenID int64) error {
	return s.db.DeleteAPIToken(tokenID)
}

// HasPermission checks if an API token has a specific permission
func (s *Service) HasPermission(apiToken *db.APIToken, resource, action string) bool {
	permission := fmt.Sprintf("convox:%s:%s", resource, action)

	for _, perm := range apiToken.Permissions {
		// Check for exact match
		if perm == permission {
			return true
		}
		// Check for wildcard matches
		if perm == "convox:*:*" || perm == fmt.Sprintf("convox:%s:*", resource) {
			return true
		}
	}

	return false
}

// DefaultCICDPermissions returns the default permissions for CI/CD tokens
func DefaultCICDPermissions() []string {
	return []string{
		"convox:apps:read",
		"convox:builds:create",
		"convox:builds:list",
		"convox:releases:list",
		"convox:releases:promote",
		"convox:ps:list",
		"convox:ps:manage",
		"convox:restart:app",
	}
}

// DefaultTokenExpiry: tokens do not expire by default (nil)
func DefaultTokenExpiry() *time.Time { return nil }
