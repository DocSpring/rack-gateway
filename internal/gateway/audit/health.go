package audit

import (
	"fmt"
	"sync/atomic"
	"time"
)

// UnhealthyAfterFailure is how long the gateway reports itself unhealthy after an audit write fails, unless a
// later write succeeds. Every request is refused while the audit log can't be written, so the health check
// failing takes the gateway out of service, loudly, instead of leaving it up refusing everything. Once the window
// ends, traffic returns and the next request tries the audit log again.
const UnhealthyAfterFailure = time.Minute

// lastWriteFailure is when the latest audit write failed (Unix nanoseconds), or 0 when the latest write succeeded.
// It's process-wide: every write, whatever writes it, goes through appendEntry.
var lastWriteFailure atomic.Int64

func noteWriteResult(err error, now time.Time) {
	if err != nil {
		lastWriteFailure.Store(now.UnixNano())
		return
	}
	lastWriteFailure.Store(0)
}

// WriteHealth returns an error while the audit log can't be written: the latest write failed less than
// UnhealthyAfterFailure before now.
func WriteHealth(now time.Time) error {
	failed := lastWriteFailure.Load()
	if failed == 0 {
		return nil
	}
	failedAt := time.Unix(0, failed)
	if now.Sub(failedAt) >= UnhealthyAfterFailure {
		return nil
	}
	return fmt.Errorf("audit log unavailable: the latest audit write failed at %s",
		failedAt.UTC().Format(time.RFC3339))
}
