package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Service handles API token operations
type Service struct {
	db *db.Database
}

var (
	// ErrAPITokenNameRequired indicates the API token name is missing after trimming.
	ErrAPITokenNameRequired = errors.New("api token name is required")
	// ErrAPITokenNameExists indicates the desired token name is already in use.
	ErrAPITokenNameExists = errors.New("api token name already exists")
)

// APITokenRequest represents a request to create an API token
type APITokenRequest struct {
	Name            string     `json:"name"`
	UserID          int64      `json:"user_id"`
	Permissions     []string   `json:"permissions"`
	ExpiresAt       *time.Time `json:"expires_at"`
	CreatedByUserID *int64     `json:"created_by_user_id,omitempty"`
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

func normalizeTokenName(name string) string {
	return strings.TrimSpace(name)
}

func (s *Service) ensureUniqueTokenName(name string, excludeID int64) error {
	exists, err := s.db.APITokenNameExists(name, excludeID)
	if err != nil {
		return fmt.Errorf("failed to check token name uniqueness: %w", err)
	}
	if exists {
		return ErrAPITokenNameExists
	}
	return nil
}

// GenerateAPIToken creates a new API token
func (s *Service) GenerateAPIToken(req *APITokenRequest) (*APITokenResponse, error) {
	name := normalizeTokenName(req.Name)
	if name == "" {
		return nil, ErrAPITokenNameRequired
	}
	if err := s.ensureUniqueTokenName(name, 0); err != nil {
		return nil, err
	}

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
	apiToken, err := s.db.CreateAPIToken(tokenHash, name, req.UserID, req.Permissions, req.ExpiresAt, req.CreatedByUserID)
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

// ListAllTokens returns all API tokens
func (s *Service) ListAllTokens() ([]*db.APIToken, error) {
	return s.db.ListAllAPITokens()
}

// DeleteToken removes an API token
func (s *Service) DeleteToken(tokenID int64) error {
	return s.db.DeleteAPIToken(tokenID)
}

// UpdateTokenName updates the display name of an API token
func (s *Service) UpdateTokenName(tokenID int64, name string) error {
	trimmed := normalizeTokenName(name)
	if trimmed == "" {
		return ErrAPITokenNameRequired
	}
	if err := s.ensureUniqueTokenName(trimmed, tokenID); err != nil {
		return err
	}
	return s.db.UpdateAPITokenName(tokenID, trimmed)
}

// UpdateTokenPermissions updates the permission list for an API token
func (s *Service) UpdateTokenPermissions(tokenID int64, permissions []string) error {
	return s.db.UpdateAPITokenPermissions(tokenID, permissions)
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
	perms := rbac.DefaultPermissionsForRole("cicd")
	if perms == nil {
		return nil
	}
	return perms
}

// DefaultTokenExpiry: tokens do not expire by default (nil)
func DefaultTokenExpiry() *time.Time { return nil }
