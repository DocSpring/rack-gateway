package jobs

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"testing"

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
