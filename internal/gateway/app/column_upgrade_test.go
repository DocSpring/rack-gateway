package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func sweepReturning(
	upgraded map[string]db.ColumnUpgradeCounts, err error,
) func(context.Context) (map[string]db.ColumnUpgradeCounts, error) {
	return func(context.Context) (map[string]db.ColumnUpgradeCounts, error) { return upgraded, err }
}

// After the rollout nothing stores plaintext secrets, so the repeat sweep encrypting one means someone wrote it
// straight into the database (a TOTP seed they know, say). That goes to Sentry, not just the log.
func TestRepeatColumnUpgradeReportsEncryptedSecrets(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	var reported []string
	report := func(message string) { reported = append(reported, message) }

	repeatColumnUpgrade(context.Background(), sweepReturning(map[string]db.ColumnUpgradeCounts{
		"slack_integration.bot_token_encrypted": {},
		"mfa_methods.secret":                    {Encrypted: 1, OtherKey: 2},
	}, nil), report)

	require.Len(t, reported, 1)
	assert.Contains(t, reported[0], "encrypted plaintext secrets (mfa_methods.secret: 1)")
	assert.Contains(t, logs.String(), "[ERROR] "+reported[0])
}

func TestRepeatColumnUpgradeIsQuietWhenNothingWasEncrypted(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	reported := 0
	report := func(string) { reported++ }

	repeatColumnUpgrade(context.Background(), sweepReturning(map[string]db.ColumnUpgradeCounts{
		"mfa_methods.secret": {OtherKey: 1, Unreadable: 1},
	}, nil), report)
	assert.Zero(t, reported, "values left alone aren't new plaintext")

	repeatColumnUpgrade(context.Background(), sweepReturning(nil, errors.New("connection refused")), report)
	assert.Zero(t, reported)
	assert.Contains(t, logs.String(), "[ERROR] Column encryption: repeat upgrade failed: connection refused")
}
