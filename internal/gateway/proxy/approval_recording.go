package proxy

import (
	"errors"
	"fmt"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// approvalRecordRetries are the waits between attempts to record a step in its deploy approval request, so a brief
// database blip doesn't fail a step the rack has carried out.
var approvalRecordRetries = []time.Duration{100 * time.Millisecond, 400 * time.Millisecond}

// approvalNotRecordedError is a step the rack carried out that the gateway couldn't record in its deploy approval
// request. Later steps find the approval by what it recorded (its object, build, release and processes), so the
// client gets a 503 saying what to do again, rather than a success the approval can't follow.
type approvalNotRecordedError struct {
	// done is what the rack did ("the rack created build B1"); retry is what the caller should do now.
	done  string
	retry string
}

func (e *approvalNotRecordedError) Error() string {
	return fmt.Sprintf("%s, but the gateway couldn't record it in the deploy approval request: %s", e.done, e.retry)
}

// recordInApproval runs write, trying again after each of approvalRecordRetries unless the approval can't take the
// record (db.ErrDeployApprovalNotRecordable: it was rejected meanwhile). It returns an approvalNotRecordedError when
// write never succeeded.
func recordInApproval(done, retry string, write func() error) error {
	err := write()
	for _, wait := range approvalRecordRetries {
		if err == nil || errors.Is(err, db.ErrDeployApprovalNotRecordable) {
			break
		}
		<-time.After(wait)
		err = write()
	}
	if err == nil {
		return nil
	}
	gtwlog.Errorf("proxy: %s, but it wasn't recorded in its deploy approval request: %v", done, err)
	return &approvalNotRecordedError{done: done, retry: retry}
}
