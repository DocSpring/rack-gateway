package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs/uptime"
	jobverifyaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/verifyaudit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type afterWriteContextKey struct{}

// A follow-up job is queued after its record is saved, so the client hanging up mustn't cancel the insert.
func TestAfterWriteContextIgnoresCallerCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), afterWriteContextKey{}, "kept"))
	cancelParent()

	ctx, cancel := afterWriteContext(parent)
	defer cancel()

	require.NoError(t, ctx.Err(), "the caller's cancellation doesn't reach the insert")
	assert.Equal(t, "kept", ctx.Value(afterWriteContextKey{}), "request values such as the trace stay")
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "the insert has its own timeout")
	assert.WithinDuration(t, time.Now().Add(afterWriteInsertTimeout), deadline, time.Second)
}

// A request whose client has already disconnected still queues the notification of the record it saved.
func TestInsertAfterWriteQueuesJobForCancelledRequest(t *testing.T) {
	t.Setenv("AUDIT_HMAC_SECRET", "")
	t.Setenv(jobverifyaudit.HeartbeatEnv, "")
	t.Setenv(uptime.URLEnv, "")
	database := dbtest.NewDatabase(t)
	client, err := NewClient(database.Pool(), &Dependencies{Database: database}, nil)
	require.NoError(t, err)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	args := jobslack.DeployApprovalArgs{DeployApprovalRequestID: 42}
	opts := &river.InsertOpts{Queue: QueueNotifications, MaxAttempts: MaxAttemptsNotification}

	_, err = client.Insert(requestCtx, args, opts)
	require.ErrorIs(t, err, context.Canceled, "a plain insert with the request's context is lost")

	result, err := client.InsertAfterWrite(requestCtx, args, opts)
	require.NoError(t, err)
	require.NotNil(t, result.Job)

	job, err := client.JobGet(context.Background(), result.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, jobslack.DeployApprovalArgs{}.Kind(), job.Kind)
	assert.Equal(t, QueueNotifications, job.Queue)
	assert.JSONEq(t, `{"deploy_approval_request_id":42}`, string(job.EncodedArgs))
}
