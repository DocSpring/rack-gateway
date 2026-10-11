package db_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	testSeed     = "JBSWY3DPEHPK3PXP"
	fakeSlackBot = "xoxb-test-token"
	mfaColumn    = "mfa_methods.secret"
	slackColumn  = "slack_integration.bot_token_encrypted"
)

func storedMFASecret(t *testing.T, database *gwdb.Database, methodID int64) string {
	t.Helper()
	var stored string
	require.NoError(t, database.DB().QueryRow(`SELECT secret FROM mfa_methods WHERE id = $1`, methodID).Scan(&stored))
	return stored
}

func storedBotToken(t *testing.T, database *gwdb.Database) string {
	t.Helper()
	var stored string
	require.NoError(t, database.DB().QueryRow(`SELECT bot_token_encrypted FROM slack_integration`).Scan(&stored))
	return stored
}

func setStoredMFASecret(t *testing.T, database *gwdb.Database, methodID int64, value string) {
	t.Helper()
	_, err := database.DB().Exec(`UPDATE mfa_methods SET secret = $1 WHERE id = $2`, value, methodID)
	require.NoError(t, err)
}

func insertRawMFAMethod(t *testing.T, database *gwdb.Database, userID int64, methodType string, secret any) int64 {
	t.Helper()
	var id int64
	err := database.DB().QueryRow(
		`INSERT INTO mfa_methods (user_id, type, label, secret, confirmed_at)
		 VALUES ($1, $2, 'Legacy', $3, NOW()) RETURNING id`,
		userID, methodType, secret,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// insertRawSlackIntegration stores a token the way an older release (or someone with database access) would.
func insertRawSlackIntegration(t *testing.T, database *gwdb.Database, storedToken string) {
	t.Helper()
	_, err := database.DB().Exec(
		`INSERT INTO slack_integration (
			workspace_id, workspace_name, bot_token_encrypted, alert_deploy_approvals_channel_id, bot_user_id, scope
		) VALUES ('T123', 'Test', $1, '', 'U123', 'chat:write')`,
		storedToken,
	)
	require.NoError(t, err)
}

func createSlackIntegration(t *testing.T, database *gwdb.Database) {
	t.Helper()
	_, err := database.CreateSlackIntegration(
		"T123", "Test Workspace", fakeSlackBot, "U123", "chat:write", map[string]interface{}{}, nil,
	)
	require.NoError(t, err)
}

func TestCreateMFAMethodStoresSecretEncrypted(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("enc@example.com", "Enc", []string{"admin"})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(user.ID, "totp", "App", testSeed, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, testSeed, method.Secret)

	stored := storedMFASecret(t, database, method.ID)
	require.Contains(t, stored, colcrypt.Prefix+"v1:")
	require.NotContains(t, stored, testSeed)

	loaded, err := database.GetMFAMethodByID(method.ID)
	require.NoError(t, err)
	require.NoError(t, loaded.SecretErr)
	require.Equal(t, testSeed, loaded.Secret)

	methods, err := database.ListAllMFAMethods(user.ID)
	require.NoError(t, err)
	require.Len(t, methods, 1)
	require.Equal(t, testSeed, methods[0].Secret)
}

func TestMFASecretCopiedToAnotherUserDoesNotDecrypt(t *testing.T) {
	database := dbtest.NewDatabase(t)
	victim, err := database.CreateUser("victim@example.com", "Victim", []string{"admin"})
	require.NoError(t, err)
	attacker, err := database.CreateUser("attacker@example.com", "Attacker", []string{"viewer"})
	require.NoError(t, err)

	victimMethod, err := database.CreateMFAMethod(victim.ID, "totp", "App", testSeed, nil, nil, nil, nil)
	require.NoError(t, err)
	attackerMethod, err := database.CreateMFAMethod(attacker.ID, "totp", "App", "KB6VQXGZLMN4Y3DC", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(attackerMethod.ID, time.Now()))

	setStoredMFASecret(t, database, attackerMethod.ID, storedMFASecret(t, database, victimMethod.ID))

	// The method is still listed, but unusable.
	methods, err := database.ListMFAMethods(attacker.ID)
	require.NoError(t, err)
	require.Len(t, methods, 1)
	require.ErrorIs(t, methods[0].SecretErr, colcrypt.ErrCorrupt)
	require.Empty(t, methods[0].Secret)
}

// A YubiKey's public ID isn't secret (it starts every OTP the key sends), so a yubiotp method switched to totp
// must not turn it into a TOTP seed someone already knows.
func TestMFASecretDoesNotDecryptAsAnotherMethodType(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("switched@example.com", "Switched", []string{"admin"})
	require.NoError(t, err)
	yubikey, err := database.CreateMFAMethod(user.ID, "yubiotp", "YubiKey", "cccccccccccc", nil, nil, nil, nil)
	require.NoError(t, err)
	_, err = database.DB().Exec(`UPDATE mfa_methods SET type = 'totp' WHERE id = $1`, yubikey.ID)
	require.NoError(t, err)

	method, err := database.GetMFAMethodByID(yubikey.ID)
	require.NoError(t, err)
	require.ErrorIs(t, method.SecretErr, colcrypt.ErrCorrupt)
	require.Empty(t, method.Secret)
}

// WebAuthn methods have no secret, so confirming one (which changes its type) leaves nothing to decrypt.
func TestWebAuthnMethodsStoreNoSecret(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("keys@example.com", "Keys", []string{"admin"})
	require.NoError(t, err)
	pending, err := database.CreateMFAMethod(user.ID, "webauthn_pending", "Security Key", "", nil, nil, nil, nil)
	require.NoError(t, err)

	var stored sql.NullString
	require.NoError(t, database.DB().QueryRow(`SELECT secret FROM mfa_methods WHERE id = $1`, pending.ID).Scan(&stored))
	require.False(t, stored.Valid)
	method, err := database.GetMFAMethodByID(pending.ID)
	require.NoError(t, err)
	require.NoError(t, method.SecretErr)
}

func TestPlaintextMFASecretIsNotUsable(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("planted@example.com", "Planted", []string{"admin"})
	require.NoError(t, err)
	method, err := database.CreateMFAMethod(user.ID, "totp", "App", testSeed, nil, nil, nil, nil)
	require.NoError(t, err)

	// Someone with database write access plants a seed they know.
	setStoredMFASecret(t, database, method.ID, "NB2WY5DPFVXHI6ZT")

	loaded, err := database.GetMFAMethodByID(method.ID)
	require.NoError(t, err)
	require.ErrorIs(t, loaded.SecretErr, colcrypt.ErrNotEncrypted)
	require.Empty(t, loaded.Secret)
}

func TestSlackBotTokenStoredEncryptedAndBoundToWorkspace(t *testing.T) {
	database := dbtest.NewDatabase(t)
	createSlackIntegration(t, database)

	stored := storedBotToken(t, database)
	require.Contains(t, stored, colcrypt.Prefix+"v1:")
	require.NotContains(t, stored, fakeSlackBot)
	require.NotContains(t, stored, base64.StdEncoding.EncodeToString([]byte(fakeSlackBot)))

	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NoError(t, integration.BotTokenErr)
	require.Equal(t, fakeSlackBot, integration.BotToken)

	_, err = database.DB().Exec(`UPDATE slack_integration SET workspace_id = 'T999'`)
	require.NoError(t, err)
	integration, err = database.GetSlackIntegration()
	require.NoError(t, err, "an undecryptable token must not hide the integration")
	require.ErrorIs(t, integration.BotTokenErr, colcrypt.ErrCorrupt)
	require.Empty(t, integration.BotToken)
}

func TestUpgradePlaintextColumns(t *testing.T) {
	database := dbtest.NewDatabase(t)
	alice, err := database.CreateUser("alice@example.com", "Alice", []string{"admin"})
	require.NoError(t, err)
	bob, err := database.CreateUser("bob@example.com", "Bob", []string{"viewer"})
	require.NoError(t, err)

	// Rows as an older release wrote them.
	legacy := map[int64]string{
		insertRawMFAMethod(t, database, alice.ID, "totp", testSeed):                      testSeed,
		insertRawMFAMethod(t, database, alice.ID, "yubiotp", "cccccccccccc"):             "cccccccccccc",
		insertRawMFAMethod(t, database, bob.ID, "webauthn", "webauthn_enroll_2_1700000"): "webauthn_enroll_2_1700000",
	}
	noSecret := insertRawMFAMethod(t, database, bob.ID, "webauthn", nil)
	current, err := database.CreateMFAMethod(bob.ID, "totp", "App", "KB6VQXGZLMN4Y3DC", nil, nil, nil, nil)
	require.NoError(t, err)
	currentStored := storedMFASecret(t, database, current.ID)

	otherCipher, err := colcrypt.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)
	otherKeyValue, err := otherCipher.Encrypt(testSeed, colcrypt.MFASecretScope(bob.ID, "totp"))
	require.NoError(t, err)
	otherKey := insertRawMFAMethod(t, database, bob.ID, "totp", otherKeyValue)

	insertRawSlackIntegration(t, database, base64.StdEncoding.EncodeToString([]byte(fakeSlackBot)))

	upgraded, err := database.UpgradePlaintextColumns(context.Background())
	require.NoError(t, err)
	require.Equal(t, gwdb.ColumnUpgradeCounts{Encrypted: 3, OtherKey: 1}, upgraded[mfaColumn])
	require.Equal(t, gwdb.ColumnUpgradeCounts{Encrypted: 1}, upgraded[slackColumn])

	for id, plaintext := range legacy {
		stored := storedMFASecret(t, database, id)
		require.True(t, colcrypt.IsEncrypted(stored))
		require.NotContains(t, stored, plaintext)
		method, err := database.GetMFAMethodByID(id)
		require.NoError(t, err)
		require.NoError(t, method.SecretErr)
		require.Equal(t, plaintext, method.Secret)
	}

	unchanged, err := database.GetMFAMethodByID(noSecret)
	require.NoError(t, err)
	require.NoError(t, unchanged.SecretErr)
	require.Empty(t, unchanged.Secret)
	require.Equal(t, currentStored, storedMFASecret(t, database, current.ID))
	require.Equal(t, otherKeyValue, storedMFASecret(t, database, otherKey))
	other, err := database.GetMFAMethodByID(otherKey)
	require.NoError(t, err)
	require.ErrorIs(t, other.SecretErr, colcrypt.ErrWrongKey)

	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.NoError(t, integration.BotTokenErr)
	require.Equal(t, fakeSlackBot, integration.BotToken)
	slackStored := storedBotToken(t, database)

	// A second run finds nothing left to do.
	again, err := database.UpgradePlaintextColumns(context.Background())
	require.NoError(t, err)
	require.Equal(t, gwdb.ColumnUpgradeCounts{OtherKey: 1}, again[mfaColumn])
	require.Equal(t, gwdb.ColumnUpgradeCounts{}, again[slackColumn])
	require.Equal(t, slackStored, storedBotToken(t, database))
}

func TestUpgradePlaintextColumnsLeavesUndecodableSlackToken(t *testing.T) {
	database := dbtest.NewDatabase(t)
	insertRawSlackIntegration(t, database, "not base64!")

	upgraded, err := database.UpgradePlaintextColumns(context.Background())
	require.NoError(t, err)
	require.Equal(t, gwdb.ColumnUpgradeCounts{Unreadable: 1}, upgraded[slackColumn])
	require.Equal(t, "not base64!", storedBotToken(t, database))

	integration, err := database.GetSlackIntegration()
	require.NoError(t, err)
	require.ErrorIs(t, integration.BotTokenErr, colcrypt.ErrNotEncrypted)
}

func TestNewDatabaseRequiresColumnEncryptionKey(t *testing.T) {
	for value, want := range map[string]error{
		"":                 colcrypt.ErrKeyMissing,
		"too-short":        colcrypt.ErrKeyInvalid,
		"c2hvcnQta2V5Cg==": colcrypt.ErrKeyInvalid,
	} {
		t.Setenv(colcrypt.EnvVar, value)
		// The key is checked before connecting, so the DSN is never used.
		_, err := gwdb.NewWithPoolConfigAndMigration("postgres://unused@127.0.0.1:1/unused", nil, false)
		require.ErrorIs(t, err, want, "key %q", value)
	}
}
