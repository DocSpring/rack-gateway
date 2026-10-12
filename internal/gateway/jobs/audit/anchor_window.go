package audit

import (
	"fmt"
	"time"

	sentry "github.com/getsentry/sentry-go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// AnchorWriteWindow is how long after its ts_utc (when its job was created) an anchor can be written. River
// retries a failing job for weeks (25 attempts, backing off attempt^4 seconds), so the job gives up on an hour
// once a retry starts later than this; the next hours' anchors cover its events. Six hours lets a failing job
// retry about nine times, and is twice verify-audit's default maximum anchor age, so an outage that stops
// anchoring fails verification well before the job gives up. verify-audit fails anchors written later than this,
// so write access to the bucket alone can't backfill an hour long after it passed.
const AnchorWriteWindow = 6 * time.Hour

// AnchorClockSkew is how far an anchor's S3 LastModified can fall outside the times its job implies. The job's
// times come from the database's clock and LastModified from S3's, and a write takes up to River's job timeout
// (a minute).
const AnchorClockSkew = 5 * time.Minute

// checkAttemptTime refuses an attempt the job's times say must not write. One that started more than
// AnchorWriteWindow after the job was created would be a backfill. One that started before the job was created
// means the creation time was moved forward in the database: it names an hour that hasn't come yet, and an anchor
// written for it now would also make the real job for that hour fail. River sets both times from the database's
// clock; a job River isn't working (no attempt time) is never refused.
func checkAttemptTime(key string, job *rivertype.JobRow) error {
	if job.AttemptedAt == nil {
		return nil
	}
	delay := job.AttemptedAt.Sub(job.CreatedAt)
	switch {
	case delay > AnchorWriteWindow:
		return fmt.Errorf("gave up on anchor %s: this attempt started %s after the job was created, past the %s "+
			"anchor write window (later hours' anchors cover its events)", key, delay.Truncate(time.Minute),
			AnchorWriteWindow)
	case delay < -AnchorClockSkew:
		return fmt.Errorf("refused to write anchor %s: the job was created at %s, after this attempt started (%s), "+
			"so its creation time was changed in the database (check who can write to it)", key,
			job.CreatedAt.UTC().Format(time.RFC3339), job.AttemptedAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// cancelAnchorJob reports err and cancels the job, for failures retrying can't fix. River doesn't pass canceled
// jobs to the client's error handler, so this logs the error and sends it to Sentry itself.
func cancelAnchorJob(err error) error {
	gtwlog.Errorf("audit anchor: %v", err)
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("job_kind", AnchorWriterArgs{}.Kind())
		sentry.CaptureException(err)
	})
	return river.JobCancel(err)
}
