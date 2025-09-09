package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OAuthHandler handles OAuth flows using vetted libraries
// Supports separate web and CLI flows with proper OIDC/OAuth2 primitives
type OAuthHandler struct {
	provider        *oidc.Provider
	oauth2ConfigCLI *oauth2.Config
	oauth2ConfigWeb *oauth2.Config
	idTokenVerifier *oidc.IDTokenVerifier
	jwtManager      *JWTManager
	allowedDomain   string
	issuingURL      string
}

// LoginStartResponse for CLI OAuth flow
type LoginStartResponse struct {
	AuthURL      string `json:"auth_url"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

// LoginResponse for successful OAuth completion
type LoginResponse struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewOAuthHandler creates a new OAuth handler using vetted OIDC libraries
func NewOAuthHandler(clientID, clientSecret, baseRedirectURL, allowedDomain, issuerURL string, jwtManager *JWTManager) (*OAuthHandler, error) {
	ctx := context.Background()

	// Use vetted OIDC provider discovery
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Configure OAuth2 for CLI flow (with PKCE)
	oauth2ConfigCLI := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  baseRedirectURL + "/.gateway/api/cli/login/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Configure OAuth2 for web flow (without PKCE)
	oauth2ConfigWeb := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  baseRedirectURL + "/.gateway/api/web/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	if os.Getenv("DEBUG_OAUTH") == "true" {
		ep := provider.Endpoint()
		log.Printf("[oauth:cfg] issuer=%s authURL=%s tokenURL=%s webRedirect=%s", issuerURL, ep.AuthURL, ep.TokenURL, oauth2ConfigWeb.RedirectURL)
	}

	// Create ID token verifier with proper configuration
	verifierConfig := &oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: []string{"RS256", "HS256"}, // Support both for flexibility
	}
	idTokenVerifier := provider.Verifier(verifierConfig)

	return &OAuthHandler{
		provider:        provider,
		oauth2ConfigCLI: oauth2ConfigCLI,
		oauth2ConfigWeb: oauth2ConfigWeb,
		idTokenVerifier: idTokenVerifier,
		jwtManager:      jwtManager,
		allowedDomain:   allowedDomain,
		issuingURL:      issuerURL,
	}, nil
}

// StartLogin initiates OAuth flow - returns auth URL and PKCE params for CLI
func (h *OAuthHandler) StartLogin() (*LoginStartResponse, error) {
	// Generate PKCE parameters using secure methods
	codeVerifier := generateSecureRandomString(128)
	codeChallenge := generatePKCEChallenge(codeVerifier)
	state := generateSecureRandomString(32)

	// Generate auth URL with PKCE
	authURL := h.oauth2ConfigCLI.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	return &LoginStartResponse{
		AuthURL:      authURL,
		State:        state,
		CodeVerifier: codeVerifier,
	}, nil
}

// StartWebLogin initiates OAuth flow for web browser - no PKCE needed
func (h *OAuthHandler) StartWebLogin() string {
	state := generateSecureRandomString(32)

	// Generate auth URL without PKCE for web flow
	authURL := h.oauth2ConfigWeb.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	if os.Getenv("DEBUG_OAUTH") == "true" {
		// Log only host + path to avoid leaking query (state, client_id)
		if u, err := url.Parse(authURL); err == nil {
			log.Printf("[oauth:web] built auth URL host=%s path=%s (query=redacted)", u.Host, u.Path)
		}
	}
	return authURL
}

// CompleteLogin handles OAuth callback - validates code and returns JWT
func (h *OAuthHandler) CompleteLogin(code, state, codeVerifier string) (*LoginResponse, error) {
	ctx := context.Background()

	// Select the right config based on whether this is CLI or web flow
	config := h.oauth2ConfigWeb
	if codeVerifier != "" {
		config = h.oauth2ConfigCLI
	}

	// Exchange code for token
	// Only include code_verifier if provided (CLI flow)
	var opts []oauth2.AuthCodeOption
	if codeVerifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	}

	token, err := config.Exchange(ctx, code, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Extract and verify ID token using vetted OIDC library
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("missing id_token in OAuth response")
	}

	idToken, err := h.idTokenVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// Extract user claims
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		HD            string `json:"hd,omitempty"` // Google Workspace domain
	}

	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	// Verify email domain if required
	if h.allowedDomain != "" && !h.isAllowedDomain(claims.Email) {
		return nil, fmt.Errorf("email domain not allowed: %s", claims.Email)
	}

	// Create JWT token using existing JWT manager
	jwtToken, expiresAt, err := h.jwtManager.GenerateToken(claims.Email, claims.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT: %w", err)
	}

	return &LoginResponse{
		Token:     jwtToken,
		Email:     claims.Email,
		Name:      claims.Name,
		ExpiresAt: expiresAt,
	}, nil
}

// isAllowedDomain checks if email domain is allowed
func (h *OAuthHandler) isAllowedDomain(email string) bool {
	if h.allowedDomain == "" {
		return true
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	return parts[1] == h.allowedDomain
}

// generateSecureRandomString generates a cryptographically secure random string
func generateSecureRandomString(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("failed to generate secure random string: %v", err))
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)
}

// generatePKCEChallenge generates PKCE code challenge from verifier
func generatePKCEChallenge(verifier string) string {
	sha := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(sha[:])
}

// (no extra wrappers; keep logging simple and guarded by env)
