// Package colcrypt encrypts individual database column values (TOTP seeds, YubiKey public IDs, the Slack
// bot token) with AES-256-GCM under a dedicated key from COLUMN_ENCRYPTION_KEY. The key is separate from
// APP_SECRET_KEY so that rotating the session secret never makes stored secrets unreadable.
//
// A stored value is an envelope:
//
//	rgwenc:v1:<kid>:<base64url-nopad(nonce || ciphertext || tag)>
//
// In the envelope:
//
//   - "v1" names the algorithm: AES-256-GCM with a random 96-bit nonce and a 128-bit tag. A different
//     algorithm gets a new version, so values written by an older version can still be told apart.
//   - kid identifies the key: the first 4 bytes of HMAC-SHA256(key, "rack-gateway column key id"), in hex.
//     It distinguishes "encrypted under another key" from "tampered with", and leaves room for key rotation
//     (reading with the previous key while writing with the new one). Rotation is not implemented yet.
//
// The additional authenticated data is "rgwenc:v1:<kid>|<table.column>|<row binding>", for example
// "rgwenc:v1:1a2b3c4d|mfa_methods.secret|user:42". A ciphertext copied to another column or to another
// user's row fails authentication, so database write access alone can't move a secret between accounts.
//
// web/e2e/column-crypto.ts implements the same format for E2E tests. A shared test vector in both test
// suites keeps the two in step.
package colcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// EnvVar names the environment variable holding the key: standard base64 of exactly 32 random bytes,
	// e.g. the output of `openssl rand -base64 32`.
	EnvVar = "COLUMN_ENCRYPTION_KEY"

	// DevKey is the published key used by development and test setups. It decodes to
	// "rack-gateway-dev-only-column-key". Everyone can read it, so the gateway refuses to start with it
	// against a database marked as production.
	DevKey = "cmFjay1nYXRld2F5LWRldi1vbmx5LWNvbHVtbi1rZXk="

	// Prefix starts every encrypted value. Values stored before encryption existed (base32 TOTP seeds,
	// YubiKey public IDs, WebAuthn enrollment placeholders, base64 Slack tokens) never contain ':'.
	Prefix = "rgwenc:"

	version  = "v1"
	keySize  = 32
	kidLabel = "rack-gateway column key id"
	kidBytes = 4
)

var (
	// ErrKeyMissing means COLUMN_ENCRYPTION_KEY is not set.
	ErrKeyMissing = errors.New(EnvVar + " is required (generate one with: openssl rand -base64 32)")
	// ErrKeyInvalid means the key is not standard base64 of exactly 32 bytes.
	ErrKeyInvalid = errors.New(
		EnvVar + " must be standard base64 of exactly 32 bytes (generate one with: openssl rand -base64 32)",
	)
	// ErrNotEncrypted means the stored value has no envelope prefix.
	ErrNotEncrypted = errors.New("colcrypt: value is not encrypted")
	// ErrWrongKey means the value was encrypted under a different key.
	ErrWrongKey = errors.New("colcrypt: value was encrypted with a different key")
	// ErrCorrupt means the value is malformed, uses an unknown version, or failed authentication (it was
	// tampered with, or copied from another column or row).
	ErrCorrupt = errors.New("colcrypt: value is corrupt or has been tampered with")
)

// Scope says where a value is stored: Column is "table.column" and Row binds the value to its row. Both
// are authenticated, so a ciphertext only decrypts in the place it was written for.
type Scope struct {
	Column string
	Row    string
}

// MFASecretScope is the scope of mfa_methods.secret, bound to the method's user.
func MFASecretScope(userID int64) Scope {
	return Scope{Column: "mfa_methods.secret", Row: "user:" + strconv.FormatInt(userID, 10)}
}

// SlackBotTokenScope is the scope of slack_integration.bot_token_encrypted, bound to the workspace.
func SlackBotTokenScope(workspaceID string) Scope {
	return Scope{Column: "slack_integration.bot_token_encrypted", Row: "workspace:" + workspaceID}
}

// Cipher encrypts and decrypts column values with one key.
type Cipher struct {
	aead   cipher.AEAD
	kid    string
	random io.Reader // nonce source: crypto/rand, except in the shared test vector
}

// New returns a Cipher for a base64-encoded key (see EnvVar).
func New(encodedKey string) (*Cipher, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return nil, ErrKeyMissing
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != keySize {
		return nil, ErrKeyInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("colcrypt: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("colcrypt: %w", err)
	}
	return &Cipher{aead: aead, kid: keyID(key), random: rand.Reader}, nil
}

func keyID(key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(kidLabel))
	return hex.EncodeToString(mac.Sum(nil)[:kidBytes])
}

// IsEncrypted reports whether value carries the envelope prefix (under any key or version).
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix)
}

// EnvelopePrefix returns the start of every value this Cipher writes: "rgwenc:v1:<kid>:".
func (c *Cipher) EnvelopePrefix() string {
	return Prefix + version + ":" + c.kid + ":"
}

// Encrypt seals plaintext for scope under a fresh random nonce.
func (c *Cipher) Encrypt(plaintext string, scope Scope) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return "", fmt.Errorf("colcrypt: generate nonce: %w", err)
	}
	payload := c.aead.Seal(nonce, nonce, []byte(plaintext), c.additionalData(scope))
	return c.EnvelopePrefix() + base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decrypt opens a value written for scope. It returns ErrNotEncrypted for a value without the prefix,
// ErrWrongKey for one encrypted under another key, and ErrCorrupt for anything else that fails. Errors
// never include the value.
func (c *Cipher) Decrypt(value string, scope Scope) (string, error) {
	rest, ok := strings.CutPrefix(value, Prefix)
	if !ok {
		return "", ErrNotEncrypted
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: malformed envelope", ErrCorrupt)
	}
	if parts[0] != version {
		return "", fmt.Errorf("%w: unsupported version", ErrCorrupt)
	}
	if parts[1] != c.kid {
		return "", fmt.Errorf("%w (key id %q, this key is %q)", ErrWrongKey, parts[1], c.kid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	nonceSize := c.aead.NonceSize()
	if err != nil || len(payload) < nonceSize+c.aead.Overhead() {
		return "", fmt.Errorf("%w: malformed payload", ErrCorrupt)
	}
	plaintext, err := c.aead.Open(nil, payload[:nonceSize], payload[nonceSize:], c.additionalData(scope))
	if err != nil {
		return "", ErrCorrupt
	}
	return string(plaintext), nil
}

func (c *Cipher) additionalData(scope Scope) []byte {
	return []byte(Prefix + version + ":" + c.kid + "|" + scope.Column + "|" + scope.Row)
}
