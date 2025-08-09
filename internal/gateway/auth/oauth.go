package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type OAuthHandler struct {
	config        *oauth2.Config
	allowedDomain string
	jwtManager    *JWTManager
	stateStore    map[string]*OAuthState
}

type OAuthState struct {
	State     string
	Challenge string
	CreatedAt time.Time
}

type GoogleUserInfo struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	HD            string `json:"hd"`
}

func NewOAuthHandler(clientID, clientSecret, redirectURL, allowedDomain string, jwtManager *JWTManager) *OAuthHandler {
	return &OAuthHandler{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		allowedDomain: allowedDomain,
		jwtManager:    jwtManager,
		stateStore:    make(map[string]*OAuthState),
	}
}

func (h *OAuthHandler) StartLogin() (*LoginStartResponse, error) {
	state := generateRandomString(32)
	codeVerifier := generateRandomString(64)
	codeChallenge := base64URLEncode(sha256Hash(codeVerifier))

	h.stateStore[state] = &OAuthState{
		State:     state,
		Challenge: codeVerifier,
		CreatedAt: time.Now(),
	}

	h.cleanupOldStates()

	authURL := h.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("access_type", "offline"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	return &LoginStartResponse{
		AuthURL:      authURL,
		State:        state,
		CodeVerifier: codeVerifier,
	}, nil
}

func (h *OAuthHandler) CompleteLogin(code, state, codeVerifier string) (*LoginResponse, error) {
	storedState, exists := h.stateStore[state]
	if !exists {
		return nil, fmt.Errorf("invalid state")
	}

	if storedState.Challenge != codeVerifier {
		return nil, fmt.Errorf("invalid code verifier")
	}

	delete(h.stateStore, state)

	ctx := context.Background()
	token, err := h.config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	userInfo, err := h.getUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	if !h.isAllowedDomain(userInfo.Email) {
		return nil, fmt.Errorf("email domain not allowed: %s", userInfo.Email)
	}

	jwtToken, err := h.jwtManager.CreateToken(userInfo.Email, userInfo.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT: %w", err)
	}

	return &LoginResponse{
		Token:     jwtToken,
		Email:     userInfo.Email,
		Name:      userInfo.Name,
		ExpiresAt: time.Now().Add(h.jwtManager.expiry),
	}, nil
}

func (h *OAuthHandler) getUserInfo(ctx context.Context, token *oauth2.Token) (*GoogleUserInfo, error) {
	client := h.config.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: %s", resp.Status)
	}

	var userInfo GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

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

func (h *OAuthHandler) cleanupOldStates() {
	cutoff := time.Now().Add(-10 * time.Minute)
	for state, info := range h.stateStore {
		if info.CreatedAt.Before(cutoff) {
			delete(h.stateStore, state)
		}
	}
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64URLEncode(b)
}

func base64URLEncode(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

type LoginStartResponse struct {
	AuthURL      string `json:"auth_url"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}
