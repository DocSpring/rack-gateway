package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeOIDCProvider serves OIDC discovery and a JWKS with one RSA key.
func fakeOIDCProvider(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/auth",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256", "HS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	return server
}

func signedToken(t *testing.T, alg, issuer string, sign func([]byte) []byte) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "kid": "k1", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"iss": issuer, "aud": "client-id", "sub": "123", "email": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sign([]byte(signingInput)))
}

// The ID token verifier accepts only RS256, so a token signed with a symmetric algorithm is refused even if
// the provider advertises it.
func TestIDTokenVerifierAcceptsOnlyRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	provider := fakeOIDCProvider(t, key)
	handler, err := NewOAuthHandler("client-id", "secret", "http://localhost:8447", "example.com", provider.URL)
	if err != nil {
		t.Fatalf("new oauth handler: %v", err)
	}

	rs256 := signedToken(t, "RS256", provider.URL, func(input []byte) []byte {
		digest := sha256.Sum256(input)
		sig, signErr := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if signErr != nil {
			t.Fatal(signErr)
		}
		return sig
	})
	if _, err := handler.idTokenVerifier.Verify(context.Background(), rs256); err != nil {
		t.Fatalf("RS256 token should verify: %v", err)
	}

	hs256 := signedToken(t, "HS256", provider.URL, func(input []byte) []byte {
		mac := hmac.New(sha256.New, []byte("guessable-secret"))
		mac.Write(input)
		return mac.Sum(nil)
	})
	_, err = handler.idTokenVerifier.Verify(context.Background(), hs256)
	if err == nil || !strings.Contains(err.Error(), `expected ["RS256"]`) {
		t.Fatalf("HS256 token must be refused because only RS256 is accepted, got err=%v", err)
	}
}
