package mfa

import (
	"errors"
	"fmt"
	"html"
	"log"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

const (
	autoLockAttempts = 5
	autoLockMinutes  = 5
)

var (
	autoLockReason = fmt.Sprintf("Automatic lock: %d failed MFA attempts in %d minutes", autoLockAttempts,
		autoLockMinutes)
	errAccountLocked = errors.New("account locked due to multiple failed attempts - contact administrator")
)

// checkAndLockAccount locks the account after autoLockAttempts failed MFA attempts in autoLockMinutes. The lock is
// audit-logged as a user.lock by the system in the same transaction; then the user and the admins are emailed.
func (s *Service) checkAndLockAccount(userID int64) error {
	failedAttempts, err := s.db.CountRecentFailedMFAAttempts(userID, autoLockMinutes)
	if err != nil {
		return fmt.Errorf("failed to count failed attempts: %w", err)
	}
	if failedAttempts < autoLockAttempts {
		return nil
	}

	locked, events, err := s.db.LockUserAudited(userID, autoLockReason, audit.UserAutoLockedEntry)
	audit.RecordedInTransaction(events, err)
	if err != nil {
		return fmt.Errorf("failed to lock account: %w", err)
	}
	if locked != nil {
		s.sendAutoLockEmails(locked)
	}
	return errAccountLocked
}

func (s *Service) sendAutoLockEmails(locked *db.LockedUser) {
	if s.emailSender == nil {
		return
	}
	reason := fmt.Sprintf("%d failed MFA attempts in %d minutes", autoLockAttempts, autoLockMinutes)
	_ = s.emailSender.Send(locked.Email, "Account Locked - Multiple Failed Login Attempts",
		"Your account has been automatically locked due to multiple failed authentication attempts.\n\n"+
			"Reason: "+reason+"\n\nIf this was not you, please contact your administrator immediately.",
		`<p><strong>Your account has been automatically locked</strong> due to multiple failed authentication `+
			`attempts.</p><p><strong>Reason:</strong> `+reason+`</p>`+
			`<p><strong style="color: #d9534f;">If this was not you, please contact your administrator `+
			`immediately.</strong></p>`)

	admins, err := s.db.UnlockedAdminEmails()
	if err != nil {
		log.Printf("mfa: couldn't email admins about the automatic lock of %s: %v", locked.Email, err)
		return
	}
	if len(admins) == 0 {
		return
	}
	_ = s.emailSender.SendMany(admins, "User locked: "+locked.Email,
		locked.Email+" was locked automatically after "+reason+".\n\n"+
			"Check with them before unlocking the account on the Users page: the attempts may not have been theirs.",
		"<p><strong>"+html.EscapeString(locked.Email)+"</strong> was locked automatically after "+reason+".</p>"+
			"<p>Check with them before unlocking the account on the Users page: the attempts may not have been "+
			"theirs.</p>")
}
