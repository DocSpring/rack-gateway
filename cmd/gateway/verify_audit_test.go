package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
)

func TestVerifyAuditFlagsDefaults(t *testing.T) {
	t.Setenv(hashV2FromEnv, "")
	parsed, err := parseVerifyAuditFlags(nil)
	require.NoError(t, err)
	assert.Equal(t, auditverify.DefaultMaxAnchorAge, parsed.maxAnchorAge)

	pinned, err := pinnedHashV2Start(parsed)
	require.NoError(t, err)
	assert.Nil(t, pinned, "the hash v2 start is only pinned when asked")
}

func TestVerifyAuditHashV2StartFromTheFlagOrTheEnvironment(t *testing.T) {
	t.Setenv(hashV2FromEnv, "42")
	parsed, err := parseVerifyAuditFlags(nil)
	require.NoError(t, err)
	pinned, err := pinnedHashV2Start(parsed)
	require.NoError(t, err)
	require.NotNil(t, pinned)
	assert.Equal(t, int64(42), *pinned)

	parsed, err = parseVerifyAuditFlags([]string{"--hash-v2-from", "0"})
	require.NoError(t, err)
	pinned, err = pinnedHashV2Start(parsed)
	require.NoError(t, err)
	require.NotNil(t, pinned)
	assert.Equal(t, int64(0), *pinned, "the flag wins over the environment")
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

	t.Setenv(hashV2FromEnv, "-5")
	parsed, err := parseVerifyAuditFlags(nil)
	require.NoError(t, err)
	_, err = pinnedHashV2Start(parsed)
	require.ErrorContains(t, err, hashV2FromEnv)
}

func TestVerifyAuditMaxAnchorAge(t *testing.T) {
	parsed, err := parseVerifyAuditFlags([]string{"--max-anchor-age", "90m"})
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, parsed.maxAnchorAge)
}
