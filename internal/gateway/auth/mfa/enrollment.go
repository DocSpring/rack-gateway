package mfa

import (
	"fmt"
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

	codes, hashes, err := s.genBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.db.ReplaceBackupCodes(userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// finalizeEnrollment confirms the method and marks user as MFA enrolled
func (s *Service) finalizeEnrollment(userID, methodID int64) error {
	now := s.now()
	if err := s.db.ConfirmMFAMethod(methodID, now); err != nil {
		return err
	}
	return s.db.SetUserMFAEnrolled(userID, true)
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
	for _, method := range methods {
		if method.Type == "yubiotp" && method.Secret == publicID {
			return fmt.Errorf("this Yubikey is already registered")
		}
	}
	return nil
}
