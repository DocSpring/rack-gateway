package jobs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"testing"

	sentry "github.com/getsentry/sentry-go"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

// Failed and panicking jobs are logged at error level, without their args.
func TestJobErrorHandlerLogsFailures(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	job := &rivertype.JobRow{
		ID: 42, Kind: "email_security", Queue: QueueSecurity, Attempt: 2, MaxAttempts: 25,
		EncodedArgs: []byte(`{"token":"rgw_secret"}`),
	}
	ctx := context.Background()
	require.Nil(t, jobErrorHandler{}.HandleError(ctx, job, errors.New("smtp: connection refused")))
	require.Nil(t, jobErrorHandler{}.HandlePanic(ctx, job, "boom", "goroutine 1 [running]"))

	out := buf.String()
	require.Contains(t, out, "job email_security failed: smtp: connection refused (id 42, attempt 2 of 25)")
	require.Contains(t, out, "job email_security panicked: boom")
	require.NotContains(t, out, "rgw_secret")
}

// Every report comes from the same line, so without a fingerprint Sentry grouped all jobs' failures in one issue.
// Each job kind's failures, and its panics, must get their own.
func TestJobErrorHandlerGroupsSentryReportsByJobKind(t *testing.T) {
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	var mu sync.Mutex
	var events []*sentry.Event
	client, err := sentry.NewClient(sentry.ClientOptions{
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
			return nil
		},
	})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	previous := hub.Client()
	hub.BindClient(client)
	t.Cleanup(func() { hub.BindClient(previous) })

	ctx := context.Background()
	approve := &rivertype.JobRow{ID: 1, Kind: "circleci:approve_job", Queue: "integrations", Attempt: 1, MaxAttempts: 5}
	ping := &rivertype.JobRow{ID: 2, Kind: "heartbeat:uptime", Queue: "default", Attempt: 1, MaxAttempts: 1}
	jobErrorHandler{}.HandleError(ctx, approve, errors.New("no workflow found"))
	jobErrorHandler{}.HandleError(ctx, ping, errors.New("ping failed"))
	jobErrorHandler{}.HandlePanic(ctx, ping, "boom", "goroutine 1 [running]")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 3)
	require.Equal(t, []string{"river-job", "failed", "circleci:approve_job"}, events[0].Fingerprint)
	require.Equal(t, []string{"river-job", "failed", "heartbeat:uptime"}, events[1].Fingerprint)
	require.Equal(t, []string{"river-job", "panicked", "heartbeat:uptime"}, events[2].Fingerprint)
	require.Equal(t, "heartbeat:uptime", events[1].Tags["job_kind"])
}
