package jobs

import (
	"context"
	"fmt"

	sentry "github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// jobErrorHandler reports failed and panicking jobs. River logs job errors at Info, below its default logger's
// Warn level, so without this a failing job (a security email, an audit anchor write) would leave no log line.
// Job args are never reported: they can hold email contents and tokens.
//
// Every report comes from the same line, so Sentry's default grouping (by stack trace) put every job's failures in
// one issue, and a new kind of failure showed up only as a regression of an unrelated one. Each job kind's failures,
// and its panics, get their own issue instead.
type jobErrorHandler struct{}

func (jobErrorHandler) HandleError(_ context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	reportJobFailure(job, "failed", fmt.Errorf("job %s failed: %w", job.Kind, err))
	return nil
}

func (jobErrorHandler) HandlePanic(
	_ context.Context, job *rivertype.JobRow, panicVal any, trace string,
) *river.ErrorHandlerResult {
	reportJobFailure(job, "panicked", fmt.Errorf("job %s panicked: %v\n%s", job.Kind, panicVal, trace))
	return nil
}

// reportJobFailure logs err and reports it to Sentry, grouped by the job's kind and outcome ("failed" or "panicked").
func reportJobFailure(job *rivertype.JobRow, outcome string, err error) {
	gtwlog.Errorf("%v (id %d, attempt %d of %d)", err, job.ID, job.Attempt, job.MaxAttempts)
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("job_kind", job.Kind)
		scope.SetTag("job_queue", job.Queue)
		scope.SetFingerprint([]string{"river-job", outcome, job.Kind})
		sentry.CaptureException(err)
	})
}
