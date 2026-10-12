package jobs

import (
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// verify-audit and the scheduled verification read the same configuration.
func TestAuditVerifyOptions(t *testing.T) {
	t.Setenv("AUDIT_HMAC_SECRET", "")
	_, err := AuditVerifyOptions(nil)
	require.ErrorIs(t, err, db.ErrAuditHMACSecretNotSet, "never the development secret")

	t.Setenv("AUDIT_HMAC_SECRET", "jobs-test-secret")
	t.Setenv(auditverify.HashV2FromEnv, "7")
	opts, err := AuditVerifyOptions(nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("jobs-test-secret"), opts.Secret)
	require.NotNil(t, opts.HashV2From)
	assert.Equal(t, int64(7), *opts.HashV2From)
	assert.Nil(t, opts.Anchors, "without anchors only the hash chain is checked")

	t.Setenv(auditverify.HashV2FromEnv, "seven")
	_, err = AuditVerifyOptions(nil)
	require.ErrorContains(t, err, auditverify.HashV2FromEnv)
}

// The latest anchor may be three anchor intervals old, and at least auditverify's default.
func TestAuditVerifyOptionsMaxAnchorAge(t *testing.T) {
	t.Setenv("AUDIT_HMAC_SECRET", "jobs-test-secret")
	t.Setenv(auditverify.HashV2FromEnv, "")
	for interval, expected := range map[time.Duration]time.Duration{
		time.Minute:   auditverify.DefaultMaxAnchorAge,
		time.Hour:     auditverify.DefaultMaxAnchorAge,
		4 * time.Hour: 12 * time.Hour,
	} {
		opts, err := AuditVerifyOptions(&AuditAnchorConfig{Bucket: "anchors", ChainID: "test", Interval: interval})
		require.NoError(t, err)
		require.NotNil(t, opts.Anchors)
		assert.Equal(t, "anchors", opts.Anchors.Bucket)
		assert.Equal(t, "test", opts.Anchors.ChainID)
		assert.Equal(t, expected, opts.Anchors.MaxAge, "anchor interval %s", interval)
	}
}

func TestAnchorIntervalFromEnv(t *testing.T) {
	for value, expected := range map[string]time.Duration{
		"":     time.Hour,
		"15":   15 * time.Minute,
		"0":    time.Hour,
		"-5":   time.Hour,
		"soon": time.Hour,
	} {
		t.Setenv("AUDIT_ANCHOR_INTERVAL_MINUTES", value)
		assert.Equal(t, expected, anchorIntervalFromEnv(), "AUDIT_ANCHOR_INTERVAL_MINUTES=%q", value)
	}
}

// The gateway only runs without AUDIT_HMAC_SECRET in DEV_MODE. Verification never uses the development secret, so
// it isn't scheduled there; with the secret it is, with or without anchors.
func TestAuditVerificationJobNeedsTheSecret(t *testing.T) {
	t.Setenv(auditverify.HashV2FromEnv, "")
	t.Setenv("AUDIT_HMAC_SECRET", "")
	job, err := auditVerificationJob(river.NewWorkers(), &Dependencies{}, nil)
	require.NoError(t, err)
	assert.Nil(t, job)

	t.Setenv("AUDIT_HMAC_SECRET", "jobs-test-secret")
	job, err = auditVerificationJob(river.NewWorkers(), &Dependencies{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, job)

	t.Setenv(auditverify.HashV2FromEnv, "seven")
	_, err = auditVerificationJob(river.NewWorkers(), &Dependencies{}, nil)
	require.ErrorContains(t, err, auditverify.HashV2FromEnv, "an invalid pin stops the gateway from starting")
}
