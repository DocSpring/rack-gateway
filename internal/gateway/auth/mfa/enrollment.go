package mfa

import (
	"errors"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// backupCodesForEnrollment returns backup codes to show while a factor is being enrolled.
// A user who is not enrolled yet gets a fresh set: their codes verify nothing until the first
// factor is confirmed, and a restarted enrollment must show codes again. An enrolled user's
// existing codes are never replaced here (regeneration is a separate step-up protected action);
// they only get codes if they have none. The database decides under the user's row lock, and
// refuses when the enrollment changed after the request was authorized.
func (s *Service) backupCodesForEnrollment(user *db.User) ([]string, error) {
	codes, hashes, err := s.genBackupCodes()
	if err != nil {
		return nil, err
	}
	issued, err := s.db.IssueEnrollmentBackupCodes(user.ID, user.MFAEnrolled, hashes)
	if errors.Is(err, db.ErrMFAEnrollmentChanged) {
		return nil, fmt.Errorf("MFA was enabled or disabled for this account meanwhile; reload and start again")
	}
	if err != nil || !issued {
		return nil, err
	}
	return codes, nil
}

// finalizeEnrollment confirms the method (storing credential, for a security key) and marks the user MFA
// enrolled, in one transaction. On a first enrollment every existing session loses its MFA-verified state: those
// sessions were marked verified at login only because the user had no factor, and must now prove one. The caller
// re-verifies the session that completed the enrollment. A method deleted meanwhile (a new enrollment started)
// enrolls nothing.
func (s *Service) finalizeEnrollment(userID, methodID int64, credential *db.WebAuthnCredential) error {
	err := s.db.FinalizeMFAEnrollment(userID, methodID, s.now(), credential)
	if errors.Is(err, db.ErrMFAMethodNotFound) {
		return fmt.Errorf("the enrollment was replaced by a newer one; start again")
	}
	return err
}

// prepareEnrollment deletes unconfirmed methods to prevent clutter
func (s *Service) prepareEnrollment(userID int64) error {
	return s.db.DeleteUnconfirmedMFAMethods(userID)
}

// checkDuplicateYubikey returns error if the public ID is already registered
func (s *Service) checkDuplicateYubikey(userID int64, publicID string) error {
	methods, err := s.db.ListAllMFAMethods(userID)
	if err != nil {
		return err
	}
	for _, method := range methodsWithSecret(methods, "yubiotp") {
		if method.Secret == publicID {
			return fmt.Errorf("this Yubikey is already registered")
		}
	}
	return nil
}
