package envutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSecretKeyMatchesCredentialNames(t *testing.T) {
	for _, key := range []string{
		"STRIPE_SECRET", "GITHUB_TOKEN", "DATABASE_PASSWORD", "admin_password", "SMTP_PASSWD", "GPG_PASSPHRASE",
		"GOOGLE_APPLICATION_CREDENTIALS", "SMTP_PASS", "DB_PASS", "SIGNING_KEY", "AWS_ACCESS_KEY_ID",
	} {
		require.True(t, IsSecretKey(key, nil), key)
	}
	for _, key := range []string{"PORT", "RAILS_ENV", "DATABASE_URL", "BYPASS_CACHE", "COMPASS_URL", "PASSENGER_APP"} {
		require.False(t, IsSecretKey(key, nil), key)
	}
	require.True(t, IsSecretKey("billing_id", []string{" BILLING_ID "}))
}

func TestHasCredentialsMatchesURLsAndConnectionStrings(t *testing.T) {
	for _, value := range []string{
		"postgres://app:hunter2@db.internal:5432/app",
		"redis://:hunter2@cache:6379/0",
		"https://user@example.com/",
		" amqp://guest:guest@mq ",
		"host=db port=5432 password=hunter2",
		"Server=db;Database=app;User Id=app;Password=hunter2;",
	} {
		require.True(t, HasCredentials(value), value)
	}
	for _, value := range []string{
		"", "3000", "postgres://db.internal:5432/app", "https://example.com/path?q=1", "user@example.com",
		"not a url", "mailto:user@example.com",
	} {
		require.False(t, HasCredentials(value), value)
	}
}

// DATABASE_URL isn't secret by name, so env:set holders can change it, but its value is still masked for callers
// that can't read secrets when it has credentials in it.
func TestMaskedForReadersChecksTheNameAndTheValue(t *testing.T) {
	require.True(t, MaskedForReaders("DATABASE_URL", "postgres://app:hunter2@db/app", nil))
	require.False(t, MaskedForReaders("DATABASE_URL", "postgres://db/app", nil))
	require.True(t, MaskedForReaders("ADMIN_PASSWORD", "hunter2", nil))
	require.True(t, MaskedForReaders("BILLING_ID", "42", []string{"BILLING_ID"}))
	require.False(t, MaskedForReaders("PORT", "3000", nil))
	require.False(t, IsSecretKey("DATABASE_URL", nil))
}
