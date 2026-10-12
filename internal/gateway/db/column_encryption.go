package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Two columns hold secrets and are encrypted with COLUMN_ENCRYPTION_KEY (see package colcrypt):
//
//   - mfa_methods.secret (TOTP seeds, YubiKey public IDs, WebAuthn enrollment placeholders), bound to user_id
//   - slack_integration.bot_token_encrypted, bound to workspace_id
//
// Reads are strict: a stored value without the envelope prefix is an error, never a usable secret. But
// UpgradePlaintextColumns, which the server runs at startup and again 10 minutes later for the upgrade to column
// encryption (v0.1.7), encrypts every plaintext value it finds. Until it is removed, someone who can write to
// the database can plant a seed they know and have it encrypted, so each value it encrypts is logged (row IDs,
// never values), and the server reports the repeat run encrypting anything to Sentry.

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

// ColumnUpgradeCounts counts what UpgradePlaintextColumns did with one column.
type ColumnUpgradeCounts struct {
	Encrypted  int // values written before encryption, now encrypted
	OtherKey   int // values encrypted under another key (or envelope version), left alone
	Unreadable int // values written before encryption that couldn't be decoded, left alone
}

// legacyColumn describes how to find and upgrade one encrypted column's values from before encryption.
type legacyColumn struct {
	name string
	// selectSQL returns (id, binding, value) for rows whose value doesn't start with the LIKE pattern $1.
	selectSQL string
	// updateSQL sets $1 on row $2 if it still holds $3.
	updateSQL string
	plaintext func(stored string) (string, error)
	scope     func(binding string) (colcrypt.Scope, error)
	// owner names whose value a row holds, for the log (never the value).
	owner func(binding string) string
}

var legacyColumns = []legacyColumn{
	{
		name: "mfa_methods.secret",
		selectSQL: `SELECT id, user_id::text || ':' || type, secret FROM mfa_methods
			WHERE secret IS NOT NULL AND secret NOT LIKE $1`,
		updateSQL: `UPDATE mfa_methods SET secret = $1 WHERE id = $2 AND secret = $3`,
		plaintext: func(stored string) (string, error) { return stored, nil },
		scope: func(binding string) (colcrypt.Scope, error) {
			user, methodType, _ := strings.Cut(binding, ":")
			userID, err := strconv.ParseInt(user, 10, 64)
			return colcrypt.MFASecretScope(userID, methodType), err
		},
		owner: func(binding string) string {
			user, methodType, _ := strings.Cut(binding, ":")
			return fmt.Sprintf("user %s, %s method", user, methodType)
		},
	},
	{
		name: "slack_integration.bot_token_encrypted",
		selectSQL: `SELECT id, workspace_id, bot_token_encrypted FROM slack_integration
			WHERE bot_token_encrypted NOT LIKE $1`,
		updateSQL: `UPDATE slack_integration SET bot_token_encrypted = $1 WHERE id = $2 AND bot_token_encrypted = $3`,
		// Tokens used to be stored base64-encoded.
		plaintext: func(stored string) (string, error) {
			token, err := base64.StdEncoding.DecodeString(stored)
			return string(token), err
		},
		scope: func(binding string) (colcrypt.Scope, error) {
			return colcrypt.SlackBotTokenScope(binding), nil
		},
		owner: func(binding string) string { return "Slack workspace " + binding },
	},
}

type legacyRow struct {
	id      int64
	binding string // "user_id:type" or workspace_id
	value   string
}

// UpgradePlaintextColumns encrypts secrets written before column encryption existed: plaintext
// mfa_methods.secret values and base64-encoded Slack bot tokens. The server runs it at startup, after
// migrations and before serving. It is not a migration because the previous release keeps serving while a
// deploy runs `migrate`, and that release can't read encrypted values.
//
// It is safe to run repeatedly and from several instances at once: each update only applies if the row
// still holds the value that was read. Values encrypted under another key are counted and left alone.
// It encrypts any plaintext value, including one written straight into the database after the upgrade, so it
// logs each row it encrypts at warn level, with the row's IDs and never its value. Remove it in a later
// release, once deployments have upgraded past v0.1.7.
func (d *Database) UpgradePlaintextColumns(ctx context.Context) (map[string]ColumnUpgradeCounts, error) {
	upgraded := make(map[string]ColumnUpgradeCounts, len(legacyColumns))
	for _, column := range legacyColumns {
		counts, err := d.upgradeColumn(ctx, column)
		if err != nil {
			return nil, fmt.Errorf("encrypt existing %s values: %w", column.name, err)
		}
		if counts != (ColumnUpgradeCounts{}) {
			gtwlog.Infof("Column encryption: %s: encrypted %d existing values, %d under another key, %d unreadable",
				column.name, counts.Encrypted, counts.OtherKey, counts.Unreadable)
		}
		upgraded[column.name] = counts
	}
	return upgraded, nil
}

func (d *Database) upgradeColumn(ctx context.Context, column legacyColumn) (ColumnUpgradeCounts, error) {
	var counts ColumnUpgradeCounts
	rows, err := d.legacyRows(ctx, column.selectSQL)
	if err != nil {
		return counts, err
	}
	for _, row := range rows {
		if err := d.upgradeRow(ctx, column, row, &counts); err != nil {
			return counts, fmt.Errorf("row %d: %w", row.id, err)
		}
	}
	return counts, nil
}

// legacyRows returns every row selectSQL finds without this key's envelope prefix. They are all read
// before any is updated.
func (d *Database) legacyRows(ctx context.Context, selectSQL string) ([]legacyRow, error) {
	// The prefix is "rgwenc:v1:<hex key id>:", which has no LIKE wildcards.
	rows, err := d.db.QueryContext(ctx, selectSQL, d.cipher.EnvelopePrefix()+"%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var found []legacyRow
	for rows.Next() {
		var row legacyRow
		if err := rows.Scan(&row.id, &row.binding, &row.value); err != nil {
			return nil, err
		}
		found = append(found, row)
	}
	return found, rows.Err()
}

// upgradeRow encrypts one row's value in place. The update goes straight to d.db rather than d.exec so the
// old value never reaches the SQL debug log.
func (d *Database) upgradeRow(
	ctx context.Context,
	column legacyColumn,
	row legacyRow,
	counts *ColumnUpgradeCounts,
) error {
	if colcrypt.IsEncrypted(row.value) {
		gtwlog.Warnf("Column encryption: %s row %d is encrypted with another column key or format; leaving it",
			column.name, row.id)
		counts.OtherKey++
		return nil
	}
	plaintext, err := column.plaintext(row.value)
	if err != nil {
		gtwlog.Warnf("Column encryption: %s row %d can't be decoded; leaving it", column.name, row.id)
		counts.Unreadable++
		return nil
	}
	scope, err := column.scope(row.binding)
	if err != nil {
		return err
	}
	encrypted, err := d.cipher.Encrypt(plaintext, scope)
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx, column.updateSQL, encrypted, row.id, row.value)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 { // 0 when another instance upgraded the row first
		gtwlog.Warnf("Column encryption: encrypted a plaintext %s value: row %d (%s)",
			column.name, row.id, column.owner(row.binding))
		counts.Encrypted++
	}
	return nil
}
