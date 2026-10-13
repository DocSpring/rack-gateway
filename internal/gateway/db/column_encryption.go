package db

import (
	"database/sql"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
)

// Two columns hold secrets and are encrypted with COLUMN_ENCRYPTION_KEY (see package colcrypt):
//
//   - mfa_methods.secret (TOTP seeds, YubiKey public IDs, WebAuthn enrollment placeholders), bound to user_id
//   - slack_integration.bot_token_encrypted, bound to workspace_id
//
// Reads are strict: a stored value without the envelope prefix is an error, never a usable secret.

// decryptMFASecret fills method.Secret, or method.SecretErr when the stored value can't be decrypted. A
// failure makes only this method unusable; it never turns into an empty secret silently. NULL means the
// method has no secret.
func (d *Database) decryptMFASecret(method *MFAMethod, stored sql.NullString) {
	if !stored.Valid {
		return
	}
	plaintext, err := d.cipher.Decrypt(stored.String, colcrypt.MFASecretScope(method.UserID, method.Type))
	if err != nil {
		method.SecretErr = fmt.Errorf("MFA method %d secret: %w", method.ID, err)
		return
	}
	method.Secret = plaintext
}
