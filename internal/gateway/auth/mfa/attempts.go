package mfa

import (
	"errors"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

const (
	// attemptLimit is the most failed (or unfinished) attempts with one kind of factor (codes, or security keys) a
	// user may have in attemptWindowMinutes (see db.ReserveMFAAttempt).
	attemptLimit         = 5
	attemptWindowMinutes = 5
)

// mfaAttempt is one MFA verification. It is recorded before the factor is checked, so that concurrent requests
// can't make more attempts than the limit allows, and counts as failed until finish records its outcome.
type mfaAttempt struct {
	record   *db.MFAAttempt
	userID   int64
	request  db.MFAAttemptRequest
	finished bool
}

// reserveAttempt records an attempt with a kind of factor (db.MFAMethodTypeTOTP or db.MFAMethodTypeWebAuthn), or
// refuses it when the user is locked or has used up their attempts.
func (s *Service) reserveAttempt(
	methodType int, userID int64, ipAddress, userAgent string, sessionID *int64,
) (*mfaAttempt, error) {
	request := db.MFAAttemptRequest{
		UserID: userID, MethodType: methodType, IPAddress: ipAddress, UserAgent: userAgent, SessionID: sessionID,
	}
	record, err := s.db.ReserveMFAAttempt(request, attemptLimit, attemptWindowMinutes)
	switch {
	case errors.Is(err, db.ErrUserLocked):
		return nil, fmt.Errorf("account locked - contact administrator")
	case errors.Is(err, db.ErrMFAAttemptLimit):
		return nil, fmt.Errorf("%w: try again in %d minutes", err, attemptWindowMinutes)
	case err != nil:
		return nil, fmt.Errorf("failed to check rate limit: %w", err)
	}
	return &mfaAttempt{record: record, userID: userID, request: request}, nil
}

// finish records the attempt's outcome.
func (a *mfaAttempt) finish(methodID *int64, success bool, reason string) {
	a.finished = true
	_ = a.record.Finish(methodID, success, reason)
}

// discard removes an attempt that couldn't have verified anything: it doesn't count toward the limit or the lock.
func (a *mfaAttempt) discard() {
	a.finished = true
	_ = a.record.Discard()
}

// finishUnlessDone records an attempt that ended with an error before its outcome was known as failed.
func (a *mfaAttempt) finishUnlessDone() {
	if !a.finished {
		a.finish(nil, false, "error")
	}
}
