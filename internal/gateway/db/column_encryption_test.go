package db_test

import (
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
