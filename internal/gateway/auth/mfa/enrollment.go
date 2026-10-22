package mfa

import (
	"errors"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ensureBackupCodes generates backup codes if the user doesn't have any yet.
// Returns backup codes only on first enrollment, nil otherwise.
func (s *Service) ensureBackupCodes(userID int64) ([]string, error) {
	existing, err := s.db.ListBackupCodes(userID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, nil
	}
	return s.GenerateBackupCodes(userID)
}

// backupCodesForEnrollment returns backup codes to show while a factor is being enrolled.
// A user who is not enrolled yet gets a fresh set: their codes protect nothing until the first
// factor is confirmed, and a restarted enrollment must show codes again. An enrolled user's
// existing codes are never replaced here (regeneration is a separate step-up protected action);
// they only get codes if they have none.
func (s *Service) backupCodesForEnrollment(user *db.User) ([]string, error) {
	if user.MFAEnrolled {
		return s.ensureBackupCodes(user.ID)
	}
	return s.GenerateBackupCodes(user.ID)
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
