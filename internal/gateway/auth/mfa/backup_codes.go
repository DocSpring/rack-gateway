package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

// GenerateBackupCodes replaces the user's backup codes and returns the plaintext set.
func (s *Service) GenerateBackupCodes(userID int64) ([]string, error) {
	codes, hashes, err := s.genBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := s.db.ReplaceBackupCodes(userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Service) genBackupCodes() ([]string, []string, error) {
	codes := make([]string, 0, backupCodeCount)
	hashes := make([]string, 0, backupCodeCount)
	for i := 0; i < backupCodeCount; i++ {
		buf := make([]byte, backupCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		code := strings.ToUpper(hex.EncodeToString(buf))
		codes = append(codes, code)
		hashes = append(hashes, s.hashBackupCode(code))
	}
	return codes, hashes, nil
}

func (s *Service) hashBackupCode(code string) string {
	mac := hmac.New(sha256.New, s.backupCodePepper)
	mac.Write([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

// normalizeBackupCode accepts a backup code the way people type it: in any case, with spaces or
// dashes between the groups. Codes are generated as uppercase hex, so this is lossless.
func normalizeBackupCode(code string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, code))
}
