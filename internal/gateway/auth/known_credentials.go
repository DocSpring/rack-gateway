package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// knownCredentialTTL is how long a credential stays known after it last authenticated.
	knownCredentialTTL = 24 * time.Hour
	// maxKnownCredentials bounds how many credentials are known at once. Only a credential that authenticated (or a
	// session the gateway created) is added, so only a gateway with that many in use fills it. Past that, a new one
	// isn't added until one expires.
	maxKnownCredentials = 10_000
)

// knownCredentials are the session and API tokens that authenticated recently, and the sessions the gateway just
// created, kept by their credentialKey. A request with one isn't refused for its address's failed attempts: another
// client behind the same NAT or proxy may have made them. Its credential is still checked in full, and one that's
// refused is forgotten, and counts as a failed attempt.
type knownCredentials struct {
	now func() time.Time
	max int

	mu sync.Mutex
	// seen is when each credential last authenticated.
	seen map[string]time.Time
}

func newKnownCredentials() *knownCredentials {
	return &knownCredentials{now: time.Now, max: maxKnownCredentials, seen: make(map[string]time.Time)}
}

// credentialKey is what a credential is known, and its attempts limited, by: its SHA-256, so the credential itself
// isn't kept.
func credentialKey(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

// has reports whether credential authenticated within knownCredentialTTL. A nil set knows nothing.
func (k *knownCredentials) has(credential string) bool {
	if k == nil || credential == "" {
		return false
	}
	key := credentialKey(credential)
	k.mu.Lock()
	defer k.mu.Unlock()
	at, ok := k.seen[key]
	return ok && k.now().Sub(at) < knownCredentialTTL
}

// remember records that credential authenticated (or was just issued) now.
func (k *knownCredentials) remember(credential string) {
	if k == nil || credential == "" {
		return
	}
	key := credentialKey(credential)
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if _, ok := k.seen[key]; !ok && len(k.seen) >= k.max {
		k.sweep(now)
		if len(k.seen) >= k.max {
			return
		}
	}
	k.seen[key] = now
}

// forget drops a credential that was refused.
func (k *knownCredentials) forget(credential string) {
	if k == nil || credential == "" {
		return
	}
	key := credentialKey(credential)
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.seen, key)
}

// sweep drops the credentials that have expired. k.mu must be held.
func (k *knownCredentials) sweep(now time.Time) {
	for key, at := range k.seen {
		if now.Sub(at) >= knownCredentialTTL {
			delete(k.seen, key)
		}
	}
}

// presentedCredential is the session or API token r authenticates with, without any inline MFA, as authenticate
// reads it: the Authorization header's, or the session cookie's when there's no header. It's "" when there's none.
func presentedCredential(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(cookie.Value)
	}
	authType, credentials, _ := strings.Cut(authHeader, " ")
	switch authType {
	case "Bearer":
		if strings.HasPrefix(credentials, "rgw_") {
			return credentials
		}
		sessionToken, _, _ := parseInlineMFA(credentials)
		return strings.TrimSpace(sessionToken)
	case "Basic":
		decoded, err := decodeBasicAuth(credentials)
		if err != nil {
			return ""
		}
		_, password, _ := strings.Cut(decoded, ":")
		authToken, _, _ := parseInlineMFA(password)
		return authToken
	default:
		return ""
	}
}
