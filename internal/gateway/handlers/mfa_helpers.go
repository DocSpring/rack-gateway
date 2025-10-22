package handlers

import "github.com/DocSpring/rack-gateway/internal/gateway/db"

// shouldEnforceMFA is a local wrapper for db.ShouldEnforceMFA to maintain backward compatibility
// within the handlers package.
func shouldEnforceMFA(settings *db.MFASettings, user *db.User) bool {
	return db.ShouldEnforceMFA(settings, user)
}

// isMFAChallengeRequired is a local wrapper for db.IsMFAChallengeRequired to maintain backward compatibility
// within the handlers package.
func isMFAChallengeRequired(settings *db.MFASettings, user *db.User) bool {
	return db.IsMFAChallengeRequired(settings, user)
}
