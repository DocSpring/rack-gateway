package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	// Email is the account's verified email, lower-cased like every user's email.
	Email string `json:"email" validate:"required"`
	Name  string `json:"name"  validate:"required"`
	// Subject is the identity provider's ID for the account (the ID token's sub claim), which never changes and
	// is never reassigned, unlike the email.
	Subject string `json:"subject" validate:"required"`
}

// NewOAuthHandler creates a new OAuth handler using vetted OIDC libraries
// The third parameter can be either a base URL (scheme+host) or a full web callback URL.
func NewOAuthHandler(
	clientID, clientSecret, redirectURLOrBase, allowedDomain, issuerURL string,
) (*OAuthHandler, error) {
	provider, err := discoverProvider(issuerURL)
	if err != nil {
		return nil, err
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
		SupportedSigningAlgs: []string{oidc.RS256},
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

	// PKCE: 32 random bytes make the 43-character verifier RFC 7636 recommends (verifiers are 43 to 128 characters).
	codeVerifier := generateSecureRandomString(32)
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

// providerHTTPClient makes every request to the identity provider (discovery, key fetches and code exchanges), so
// a provider that accepts a connection and then stalls can't hold a request, or the gateway's startup, for long.
var providerHTTPClient = &http.Client{Timeout: 15 * time.Second}

// loginCompletionTimeout bounds the code exchange and ID token verification of one login.
const loginCompletionTimeout = 30 * time.Second

// CompleteLogin handles OAuth callback - validates code and returns user info. ctx is the callback request's: the
// provider requests stop when the client goes away.
func (h *OAuthHandler) CompleteLogin(
	ctx context.Context, code, _ string, codeVerifier string,
) (*LoginResponse, error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, providerHTTPClient), loginCompletionTimeout)
	defer cancel()

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

	var claims identityClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}
	if err := claims.validate(h.allowedDomain); err != nil {
		return nil, err
	}

	return &LoginResponse{
		Email:   strings.ToLower(strings.TrimSpace(claims.Email)),
		Name:    claims.Name,
		Subject: claims.Subject,
	}, nil
}

// identityClaims are the Google ID token claims the gateway relies on.
type identityClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	HD            string `json:"hd,omitempty"` // Google Workspace hosted domain; absent for consumer accounts
}

// validate requires a subject, a verified email and, when an allowed domain is configured, that both the email
// domain and the hosted-domain claim match it.
func (c identityClaims) validate(allowedDomain string) error {
	if strings.TrimSpace(c.Subject) == "" {
		return errors.New("the ID token has no subject")
	}
	if !c.EmailVerified {
		return fmt.Errorf("google account email %q is not verified", c.Email)
	}
	if allowedDomain == "" {
		return nil
	}
	parts := strings.Split(c.Email, "@")
	if len(parts) != 2 || !strings.EqualFold(parts[1], allowedDomain) || !strings.EqualFold(c.HD, allowedDomain) {
		return &DomainNotAllowedError{Email: c.Email, Name: c.Name}
	}
	return nil
}

// discoverProvider runs OIDC discovery, retrying with exponential backoff (at most 2 seconds apart) for up to 20
// seconds while the identity provider starts.
func discoverProvider(issuerURL string) (*oidc.Provider, error) {
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	backoff := 100 * time.Millisecond
	for attempt := 1; ; attempt++ {
		provider, err := oidc.NewProvider(oidc.ClientContext(deadline, providerHTTPClient), issuerURL)
		if err == nil {
			if attempt > 1 {
				log.Printf("Successfully connected to OIDC provider after %d attempts", attempt)
			}
			return provider, nil
		}
		if deadline.Err() != nil {
			return nil, fmt.Errorf("failed to create OIDC provider after %d attempts: %w", attempt, err)
		}

		log.Printf("OIDC provider not ready (attempt %d), retrying in %v: %v", attempt, backoff, err)
		timer := time.NewTimer(backoff)
		select {
		case <-deadline.Done():
		case <-timer.C:
		}
		timer.Stop()
		backoff = min(backoff*2, 2*time.Second)
	}
}

// generateSecureRandomString base64url-encodes length random bytes. crypto/rand.Read never returns an error: it
// crashes the program if the system can't supply randomness.
func generateSecureRandomString(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
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
	if strings.HasSuffix(p, "/api/v1/auth/web/callback") {
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
