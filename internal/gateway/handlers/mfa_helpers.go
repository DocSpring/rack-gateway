package handlers

import "github.com/DocSpring/convox-gateway/internal/gateway/db"

// shouldEnforceMFA returns true when the user is subject to MFA enforcement
// (e.g. require_all_users policy or a per-user enforcement flag).
func shouldEnforceMFA(settings *db.MFASettings, user *db.User) bool {
	if user == nil {
		if settings == nil {
			return true
		}
		return settings.RequireAllUsers
	}
	if settings == nil {
		return true
	}
	if settings.RequireAllUsers {
		return true
	}
	return user.MFAEnforcedAt != nil
}

// isMFAChallengeRequired returns true when the user must complete an MFA
// challenge to proceed (i.e. enforcement is active and the user is enrolled).
func isMFAChallengeRequired(settings *db.MFASettings, user *db.User) bool {
	if user == nil {
		return false
	}
	if !user.MFAEnrolled {
		return false
	}
	return shouldEnforceMFA(settings, user)
}
