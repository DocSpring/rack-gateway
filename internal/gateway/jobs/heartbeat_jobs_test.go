package jobs

import (
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs/uptime"
	jobverifyaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/verifyaudit"
)

func TestUptimeHeartbeatJob(t *testing.T) {
	t.Setenv(uptime.URLEnv, "")
	t.Setenv(uptime.IntervalEnv, "")
	job, err := uptimeHeartbeatJob(river.NewWorkers())
	require.NoError(t, err)
	assert.Nil(t, job, "no URL, no heartbeat")

	t.Setenv(uptime.URLEnv, "https://cronitor.link/p/key/gateway-uptime")
	job, err = uptimeHeartbeatJob(river.NewWorkers())
	require.NoError(t, err)
	assert.NotNil(t, job)

	t.Setenv(uptime.IntervalEnv, "never")
	_, err = uptimeHeartbeatJob(river.NewWorkers())
	require.ErrorContains(t, err, uptime.IntervalEnv, "an invalid interval stops the gateway from starting")

	t.Setenv(uptime.IntervalEnv, "")
	t.Setenv(uptime.URLEnv, "http://cronitor.link/p/key/gateway-uptime")
	_, err = uptimeHeartbeatJob(river.NewWorkers())
	require.ErrorContains(t, err, uptime.URLEnv, "an invalid URL stops the gateway from starting")
}

func TestAuditVerificationJobHeartbeatURL(t *testing.T) {
	t.Setenv(auditverify.HashV2FromEnv, "")
	t.Setenv("AUDIT_HMAC_SECRET", "jobs-test-secret")
	t.Setenv(jobverifyaudit.HeartbeatEnv, "https://cronitor.link/p/key/audit-verify")
	job, err := auditVerificationJob(river.NewWorkers(), &Dependencies{}, nil)
	require.NoError(t, err)
	assert.NotNil(t, job)

	t.Setenv(jobverifyaudit.HeartbeatEnv, "cronitor.link/p/key/audit-verify")
	_, err = auditVerificationJob(river.NewWorkers(), &Dependencies{}, nil)
	require.ErrorContains(t, err, jobverifyaudit.HeartbeatEnv, "an invalid URL stops the gateway from starting")
}
