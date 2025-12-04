package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// DomainNotAllowedError is returned when a user's email domain is not allowed
type DomainNotAllowedError struct {
	Email string
	Name  string
}

func (e *DomainNotAllowedError) Error() string {
	return fmt.Sprintf("email domain not allowed: %s", e.Email)
}

// OAuthHandler handles OAuth flows using vetted libraries
// Supports separate web and CLI flows with proper OIDC/OAuth2 primitives
type OAuthHandler struct {
	provider        *oidc.Provider
	oauth2ConfigCLI *oauth2.Config
	oauth2ConfigWeb *oauth2.Config
	idTokenVerifier *oidc.IDTokenVerifier
	allowedDomain   string
	issuingURL      string
}

// LoginStartResponse for CLI OAuth flow
type LoginStartResponse struct {
	AuthURL      string `json:"auth_url"      validate:"required"`
	State        string `json:"state"         validate:"required"`
	CodeVerifier string `json:"code_verifier" validate:"required"`
}

// LoginResponse for successful OAuth completion
type LoginResponse struct {
	Email string `json:"email" validate:"required"`
	Name  string `json:"name"  validate:"required"`
}

// NewOAuthHandler creates a new OAuth handler using vetted OIDC libraries
// The third parameter can be either a base URL (scheme+host) or a full web callback URL.
func NewOAuthHandler(
	clientID, clientSecret, redirectURLOrBase, allowedDomain, issuerURL string,
) (*OAuthHandler, error) {
	ctx := context.Background()

	// Use vetted OIDC provider discovery with exponential backoff retry
	var provider *oidc.Provider
	var err error

	maxDuration := 20 * time.Second
	startTime := time.Now()
	backoff := 100 * time.Millisecond
	attempt := 1

	for {
		provider, err = oidc.NewProvider(ctx, issuerURL)
		if err == nil {
			if attempt > 1 {
				log.Printf("Successfully connected to OIDC provider after %d attempts", attempt)
			}
			break
		}

		elapsed := time.Since(startTime)
		if elapsed >= maxDuration {
			return nil, fmt.Errorf("failed to create OIDC provider after %v: %w", elapsed, err)
		}

		log.Printf("OIDC provider not ready (attempt %d), retrying in %v: %v", attempt, backoff, err)
		time.Sleep(backoff)

		// Exponential backoff with max 2 second delay
		backoff = time.Duration(math.Min(float64(backoff*2), float64(2*time.Second)))
		attempt++
	}

	// Derive redirects from provided value (accept base or full web callback)
	_, webRedirect, cliRedirect, err := deriveRedirects(redirectURLOrBase)
	if err != nil {
		return nil, err
	}

	// Configure OAuth2 for CLI flow (with PKCE)
	oauth2ConfigCLI := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  cliRedirect,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Configure OAuth2 for web flow (without PKCE)
	oauth2ConfigWeb := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  webRedirect,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	// optional: log at debug level happens in main

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
		allowedDomain:   allowedDomain,
		issuingURL:      issuerURL,
	}, nil
}

// StartLogin initiates OAuth flow - returns auth URL and PKCE params for CLI
func (h *OAuthHandler) StartLogin() (*LoginStartResponse, error) {
	if h == nil || h.oauth2ConfigCLI == nil {
		return nil, fmt.Errorf("oauth handler not configured for CLI login")
	}

	// Generate PKCE parameters using secure methods
	codeVerifier := generateSecureRandomString(128)
	codeChallenge := generatePKCEChallenge(codeVerifier)
	state := generateSecureRandomString(32)

	// Build auth URL options
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}

	// Add hd (hosted domain) parameter to filter Google accounts shown during sign-in
	if h.allowedDomain != "" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", h.allowedDomain))
	}

	authURL := h.oauth2ConfigCLI.AuthCodeURL(state, opts...)

	return &LoginStartResponse{
		AuthURL:      authURL,
		State:        state,
		CodeVerifier: codeVerifier,
	}, nil
}

// StartWebLogin initiates OAuth flow for web browser - no PKCE needed.
// Returns the auth URL along with the opaque state value so callers can
// persist it for later validation during the callback.
func (h *OAuthHandler) StartWebLogin() (authURL string, state string) {
	state = generateSecureRandomString(32)

	// Build auth URL options
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}

	// Add hd (hosted domain) parameter to filter Google accounts shown during sign-in
	if h.allowedDomain != "" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", h.allowedDomain))
	}

	authURL = h.oauth2ConfigWeb.AuthCodeURL(state, opts...)

	return authURL, state
}

// CompleteLogin handles OAuth callback - validates code and returns user info
func (h *OAuthHandler) CompleteLogin(code, _ string, codeVerifier string) (*LoginResponse, error) {
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
		return nil, &DomainNotAllowedError{
			Email: claims.Email,
			Name:  claims.Name,
		}
	}

	return &LoginResponse{
		Email: claims.Email,
		Name:  claims.Name,
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

// deriveRedirects builds web and CLI redirect URIs from a provided base or full web callback
func deriveRedirects(input string) (base string, web string, cli string, err error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", "", "", fmt.Errorf("missing domain: cannot derive OAuth redirect URLs")
	}
	u, perr := url.Parse(in)
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return "", "", "", fmt.Errorf("invalid redirect base: must include scheme and host")
	}
	base = u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(p, "/api/v1/auth/web/callback") || strings.HasSuffix(p, "/app/auth/callback") {
		// Treat input as full web callback URL
		web = u.String()
		cli = base + "/api/v1/auth/cli/callback"
		return base, web, cli, err
	}
	// Treat input as base URL
	web = base + "/api/v1/auth/web/callback"
	cli = base + "/api/v1/auth/cli/callback"
	return base, web, cli, err
}
