package audit

import (
	"errors"
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

// ErrUnexpectedAnchor is returned when an hour's anchor key already holds an object that S3 stored before the
// hour began. The writer never writes a key before its hour, so someone with write access to the bucket created
// it ahead of time. While Object Lock keeps it, that hour can't be anchored.
var ErrUnexpectedAnchor = errors.New("anchor key was written before its hour began")

// checkExistingAnchor accepts the existing anchor for the hour of ts, unless S3 stored it before that hour began.
func checkExistingAnchor(key string, ts, written time.Time) error {
	hourStart := ts.UTC().Truncate(time.Hour)
	if !written.Before(hourStart.Add(-AnchorClockSkew)) {
		return nil
	}
	return fmt.Errorf("%w: %s was stored at %s, so the anchor writer didn't write it and this hour can't be "+
		"anchored (check who can write to the bucket)", ErrUnexpectedAnchor, key, written.UTC().Format(time.RFC3339))
}

// pastWriteWindow reports whether the job's current attempt started more than AnchorWriteWindow after the job was
// created, and how long after. River sets both times from the database's clock; a job that isn't being worked by
// River (no attempt time) is never late.
func pastWriteWindow(job *rivertype.JobRow) (time.Duration, bool) {
	if job.AttemptedAt == nil {
		return 0, false
	}
	delay := job.AttemptedAt.Sub(job.CreatedAt)
	return delay, delay > AnchorWriteWindow
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
