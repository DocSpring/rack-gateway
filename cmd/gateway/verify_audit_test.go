package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
)

// verifyAuditOptionsFor parses args and builds the options with AUDIT_HMAC_SECRET set.
func verifyAuditOptionsFor(t *testing.T, args ...string) (auditverify.Options, error) {
	t.Helper()
	t.Setenv("AUDIT_HMAC_SECRET", "cli-test-secret")
	parsed, err := parseVerifyAuditFlags(args)
	require.NoError(t, err)
	return verifyAuditOptions(parsed)
}

func TestVerifyAuditFlagsDefaults(t *testing.T) {
	t.Setenv(auditverify.HashV2FromEnv, "")
	parsed, err := parseVerifyAuditFlags(nil)
	require.NoError(t, err)
	assert.Zero(t, parsed.maxAnchorAge, "the maximum anchor age comes from the configuration unless given")

	opts, err := verifyAuditOptionsFor(t, "--no-anchors")
	require.NoError(t, err)
	assert.Nil(t, opts.HashV2From, "the hash v2 start is only pinned when asked")
	assert.Nil(t, opts.Anchors)
	assert.Nil(t, opts.To)
	assert.Equal(t, []byte("cli-test-secret"), opts.Secret)
}

func TestVerifyAuditHashV2StartFromTheFlagOrTheEnvironment(t *testing.T) {
	t.Setenv(auditverify.HashV2FromEnv, "42")
	opts, err := verifyAuditOptionsFor(t, "--no-anchors")
	require.NoError(t, err)
	require.NotNil(t, opts.HashV2From)
	assert.Equal(t, int64(42), *opts.HashV2From)

	opts, err = verifyAuditOptionsFor(t, "--no-anchors", "--hash-v2-from", "0")
	require.NoError(t, err)
	require.NotNil(t, opts.HashV2From)
	assert.Equal(t, int64(0), *opts.HashV2From, "the flag wins over the environment")
}

func TestVerifyAuditRejectsInvalidValues(t *testing.T) {
	for _, args := range [][]string{
		{"--hash-v2-from", "-1"},
		{"--hash-v2-from", "abc"},
		{"--max-anchor-age", "0s"},
		{"--max-anchor-age", "-1h"},
		{"extra"},
	} {
		_, err := parseVerifyAuditFlags(args)
		assert.Error(t, err, "%v", args)
	}

	t.Setenv(auditverify.HashV2FromEnv, "-5")
	_, err := verifyAuditOptionsFor(t, "--no-anchors")
	require.ErrorContains(t, err, auditverify.HashV2FromEnv)
}

func TestVerifyAuditMaxAnchorAge(t *testing.T) {
	t.Setenv("AUDIT_ANCHOR_BUCKET", "anchors")
	t.Setenv("AUDIT_ANCHOR_CHAIN_ID", "test")
	t.Setenv("AUDIT_ANCHOR_INTERVAL_MINUTES", "")
	t.Setenv(auditverify.HashV2FromEnv, "")

	opts, err := verifyAuditOptionsFor(t)
	require.NoError(t, err)
	require.NotNil(t, opts.Anchors)
	assert.Equal(t, auditverify.DefaultMaxAnchorAge, opts.Anchors.MaxAge)

	opts, err = verifyAuditOptionsFor(t, "--max-anchor-age", "90m")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, opts.Anchors.MaxAge)
}

func TestVerifyAuditNeedsAnchorsOrNoAnchors(t *testing.T) {
	t.Setenv("AUDIT_ANCHOR_BUCKET", "")
	_, err := verifyAuditOptionsFor(t)
	require.ErrorContains(t, err, "--no-anchors")
}
