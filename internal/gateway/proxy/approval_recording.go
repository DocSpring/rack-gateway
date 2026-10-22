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

// inApproval is what recordInApproval records a step in.
const inApproval = "record it in the deploy approval request"

// stepNotRecordedError is a step the rack carried out that the gateway couldn't record: in its deploy approval
// request, which later steps find by what it recorded (its object, build, release and processes), or as an
// uploaded object, which a build needs. The client gets a 503 saying what to do again, rather than a success the
// gateway can't follow.
type stepNotRecordedError struct {
	// done is what the rack did ("the rack created build B1"); unrecorded is what the gateway couldn't do ("record it
	// in the deploy approval request"); retry is what the caller should do now.
	done       string
	unrecorded string
	retry      string
}

func (e *stepNotRecordedError) Error() string {
	return fmt.Sprintf("%s, but the gateway couldn't %s: %s", e.done, e.unrecorded, e.retry)
}

// recordInApproval runs write, which records a step in its deploy approval request, as recordStep does.
func recordInApproval(done, retry string, write func() error) error {
	return recordStep(done, inApproval, retry, write)
}

// recordStep runs write, trying again after each of approvalRecordRetries unless the approval can't take the record
// (db.ErrDeployApprovalNotRecordable: it was rejected meanwhile). It returns a stepNotRecordedError when write never
// succeeded.
func recordStep(done, unrecorded, retry string, write func() error) error {
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
	gtwlog.Errorf("proxy: %s, but the gateway couldn't %s: %v", done, unrecorded, err)
	return &stepNotRecordedError{done: done, unrecorded: unrecorded, retry: retry}
}

// unreadableAnswer is a step the rack answered with a success the gateway can't follow: the answer doesn't name what
// the rack created (the build's ID, the stored object's URL, the process's ID), so the step can't be recorded.
func unreadableAnswer(done, retry string) error {
	gtwlog.Errorf("proxy: %s", done)
	return &stepNotRecordedError{done: done, unrecorded: "tell what to record", retry: retry}
}
