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
type jobErrorHandler struct{}

func (jobErrorHandler) HandleError(_ context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	reportJobFailure(job, fmt.Errorf("job %s failed: %w", job.Kind, err))
	return nil
}

func (jobErrorHandler) HandlePanic(
	_ context.Context, job *rivertype.JobRow, panicVal any, trace string,
) *river.ErrorHandlerResult {
	reportJobFailure(job, fmt.Errorf("job %s panicked: %v\n%s", job.Kind, panicVal, trace))
	return nil
}

func reportJobFailure(job *rivertype.JobRow, err error) {
	gtwlog.Errorf("%v (id %d, attempt %d of %d)", err, job.ID, job.Attempt, job.MaxAttempts)
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("job_kind", job.Kind)
		scope.SetTag("job_queue", job.Queue)
		sentry.CaptureException(err)
	})
}
