package colcrypt

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The shared test vector. web/src/test/column-crypto.test.ts asserts the same envelope for the same
// inputs, so the Go and TypeScript implementations can't drift apart.
const (
	vectorKey       = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" // bytes 0x00..0x1f
	vectorNonce     = "a0a1a2a3a4a5a6a7a8a9aaab"
	vectorPlaintext = "JBSWY3DPEHPK3PXP"
	vectorUserID    = 42
	vectorEnvelope  = "rgwenc:v1:c2fa4cfe:oKGio6SlpqeoqaqrrFovehz4Ru8nLdeYNCqYjj75XvCHi5qUNLQy-T7NX2E"
)

const otherKey = "//////////////////////////////////////////8=" // 32 bytes of 0xff

func mustCipher(t *testing.T, key string) *Cipher {
	t.Helper()
	c, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func mustEncrypt(t *testing.T, c *Cipher, plaintext string, scope Scope) string {
	t.Helper()
	envelope, err := c.Encrypt(plaintext, scope)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return envelope
}

func TestSharedTestVector(t *testing.T) {
	c := mustCipher(t, vectorKey)
	nonce, err := hex.DecodeString(vectorNonce)
	if err != nil {
		t.Fatal(err)
	}
	c.random = bytes.NewReader(nonce)
	if got := mustEncrypt(t, c, vectorPlaintext, MFASecretScope(vectorUserID, "totp")); got != vectorEnvelope {
		t.Fatalf("envelope mismatch:\n got  %s\n want %s", got, vectorEnvelope)
	}
	plaintext, err := c.Decrypt(vectorEnvelope, MFASecretScope(vectorUserID, "totp"))
	if err != nil || plaintext != vectorPlaintext {
		t.Fatalf("Decrypt = %q, %v; want %q", plaintext, err, vectorPlaintext)
	}
}

func TestRoundTrip(t *testing.T) {
	c := mustCipher(t, DevKey)
	for _, plaintext := range []string{"", "JBSWY3DPEHPK3PXP", "xoxb-1234-abcd", "with:colons|and|pipes"} {
		envelope := mustEncrypt(t, c, plaintext, MFASecretScope(7, "totp"))
		if !strings.HasPrefix(envelope, c.EnvelopePrefix()) || !IsEncrypted(envelope) {
			t.Fatalf("envelope %q lacks prefix %q", envelope, c.EnvelopePrefix())
		}
		if plaintext != "" && strings.Contains(envelope, plaintext) {
			t.Fatalf("envelope contains the plaintext")
		}
		got, err := c.Decrypt(envelope, MFASecretScope(7, "totp"))
		if err != nil || got != plaintext {
			t.Fatalf("Decrypt = %q, %v; want %q", got, err, plaintext)
		}
	}
}

func TestEncryptionsDiffer(t *testing.T) {
	c := mustCipher(t, DevKey)
	first := mustEncrypt(t, c, "same", MFASecretScope(1, "totp"))
	second := mustEncrypt(t, c, "same", MFASecretScope(1, "totp"))
	if first == second {
		t.Fatal("two encryptions of the same value must use different nonces")
	}
}

func TestDecryptRejectsOtherScopes(t *testing.T) {
	c := mustCipher(t, DevKey)
	envelope := mustEncrypt(t, c, "JBSWY3DPEHPK3PXP", MFASecretScope(1, "totp"))

	for name, scope := range map[string]Scope{
		"another user":   MFASecretScope(2, "totp"),
		"another type":   MFASecretScope(1, "yubiotp"),
		"another column": {Column: "slack_integration.bot_token_encrypted", Row: "user:1"},
		"slack scope":    SlackBotTokenScope("1"),
	} {
		if _, err := c.Decrypt(envelope, scope); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err = %v, want ErrCorrupt", name, err)
		}
	}
}

func TestDecryptRejectsTamperedPayload(t *testing.T) {
	c := mustCipher(t, DevKey)
	envelope := mustEncrypt(t, c, "JBSWY3DPEHPK3PXP", MFASecretScope(1, "totp"))
	encoded := strings.TrimPrefix(envelope, c.EnvelopePrefix())
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}

	for _, index := range []int{0, 12, len(payload) - 1} { // nonce, ciphertext, tag
		tampered := append([]byte(nil), payload...)
		tampered[index] ^= 0x01
		value := c.EnvelopePrefix() + base64.RawURLEncoding.EncodeToString(tampered)
		if _, err := c.Decrypt(value, MFASecretScope(1, "totp")); !errors.Is(err, ErrCorrupt) {
			t.Errorf("byte %d flipped: err = %v, want ErrCorrupt", index, err)
		}
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	envelope := mustEncrypt(t, mustCipher(t, DevKey), "JBSWY3DPEHPK3PXP", MFASecretScope(1, "totp"))
	other := mustCipher(t, otherKey)
	if _, err := other.Decrypt(envelope, MFASecretScope(1, "totp")); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("err = %v, want ErrWrongKey", err)
	}

	// A value relabelled with this key's id still fails authentication.
	relabelled := other.EnvelopePrefix() + strings.TrimPrefix(envelope, mustCipher(t, DevKey).EnvelopePrefix())
	if _, err := other.Decrypt(relabelled, MFASecretScope(1, "totp")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("relabelled: err = %v, want ErrCorrupt", err)
	}
}

func TestDecryptRejectsUnencryptedAndMalformedValues(t *testing.T) {
	c := mustCipher(t, DevKey)
	cases := map[string]error{
		"JBSWY3DPEHPK3PXP":         ErrNotEncrypted,
		"":                         ErrNotEncrypted,
		"eG94Yi10b2tlbg==":         ErrNotEncrypted,
		"rgwenc:":                  ErrCorrupt,
		"rgwenc:v1:" + c.kid:       ErrCorrupt,
		"rgwenc:v2:" + c.kid + ":": ErrCorrupt,
		c.EnvelopePrefix():         ErrCorrupt,
		c.EnvelopePrefix() + "!!":  ErrCorrupt,
		c.EnvelopePrefix() + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA": ErrCorrupt,
	}
	for value, want := range cases {
		if _, err := c.Decrypt(value, MFASecretScope(1, "totp")); !errors.Is(err, want) {
			t.Errorf("Decrypt(%q) err = %v, want %v", value, err, want)
		}
	}
}

func TestNewRejectsMalformedKeys(t *testing.T) {
	cases := map[string]error{
		"":          ErrKeyMissing,
		"   ":       ErrKeyMissing,
		"not-a-key": ErrKeyInvalid,
		base64.StdEncoding.EncodeToString(make([]byte, 16)):               ErrKeyInvalid,
		base64.StdEncoding.EncodeToString(make([]byte, 33)):               ErrKeyInvalid,
		base64.RawStdEncoding.EncodeToString(make([]byte, 32)):            ErrKeyInvalid,
		base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32)): ErrKeyInvalid,
		hex.EncodeToString(make([]byte, 32)):                              ErrKeyInvalid,
	}
	for key, want := range cases {
		if _, err := New(key); !errors.Is(err, want) {
			t.Errorf("New(%q) err = %v, want %v", key, err, want)
		}
	}
}

func TestNewAcceptsSurroundingWhitespace(t *testing.T) {
	c := mustCipher(t, " "+DevKey+"\n")
	if c.kid != mustCipher(t, DevKey).kid {
		t.Fatal("whitespace around the key must not change it")
	}
}

// Confirming a WebAuthn method changes its type from webauthn_pending to webauthn; both have the same scope.
func TestPendingAndConfirmedWebAuthnShareAScope(t *testing.T) {
	if MFASecretScope(1, "webauthn_pending") != MFASecretScope(1, "webauthn") {
		t.Fatal("pending and confirmed WebAuthn methods have different scopes")
	}
}

// Base64 decoding ignores line breaks, so a wrapped or padded development key must still be recognized.
func TestIsDevelopmentKey(t *testing.T) {
	for _, key := range []string{DevKey, " " + DevKey + "\n", DevKey[:20] + "\n" + DevKey[20:]} {
		if !IsDevelopmentKey(key) {
			t.Errorf("IsDevelopmentKey(%q) = false", key)
		}
	}
	for _, key := range []string{"", "not base64", vectorKey} {
		if IsDevelopmentKey(key) {
			t.Errorf("IsDevelopmentKey(%q) = true", key)
		}
	}
}
