package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/convox"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routematch"
)

// verifyMFAIfRequired checks if MFA is required for the route and verifies inline MFA if provided
func (h *Handler) verifyMFAIfRequired(r *http.Request, w http.ResponseWriter, authUser *auth.AuthUser, resource rbac.Resource, action rbac.Action, rackConfig *config.RackConfig, start time.Time) error {
	// Skip MFA verification if services are not configured (e.g., in tests)
	if h.mfaService == nil || h.sessionManager == nil {
		return nil
	}

	// Get the route's MFA requirement
	permission := fmt.Sprintf("convox:%s:%s", resource.String(), action.String())

	// Look up the MFA level for this permission
	// If not explicitly defined, default to MFANone (read-only operations don't require MFA)
	mfaLevel, ok := routematch.GetMFALevelForPermission(permission)
	if !ok {
		mfaLevel = convox.MFANone
	}

	// No MFA required
	if mfaLevel == convox.MFANone {
		return nil
	}

	// Check if MFA was provided inline (for CLI requests)
	if authUser.MFAType != "" && authUser.MFAValue != "" {
		// Verify the inline MFA
		userRecord, err := h.database.GetUser(authUser.Email)
		if err != nil {
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusInternalServerError, time.Since(start), fmt.Errorf("failed to get user: %w", err))
			http.Error(w, "failed to verify MFA", http.StatusInternalServerError)
			return fmt.Errorf("failed to get user")
		}

		// Verify based on MFA type
		var verifyErr error
		switch authUser.MFAType {
		case "totp":
			if h.mfaService != nil {
				var sessionIDPtr *int64
				if authUser.Session != nil {
					sessionIDPtr = &authUser.Session.ID
				}
				_, verifyErr = h.mfaService.VerifyTOTP(userRecord, authUser.MFAValue, clientIPFromRequest(r), r.UserAgent(), sessionIDPtr)
			} else {
				verifyErr = fmt.Errorf("MFA service not available")
			}
		case "webauthn":
			if h.mfaService != nil {
				// Decode the base64-encoded assertion data
				assertionJSON, err := base64.StdEncoding.DecodeString(authUser.MFAValue)
				if err != nil {
					verifyErr = fmt.Errorf("invalid webauthn assertion format: %w", err)
				} else {
					// Parse the assertion JSON
					var assertionData struct {
						SessionData string `json:"session_data"`
						Assertion   struct {
							CredentialID      string `json:"credential_id"`
							AuthenticatorData string `json:"authenticator_data"`
							ClientDataJSON    string `json:"client_data_json"`
							Signature         string `json:"signature"`
							UserHandle        string `json:"user_handle"`
						} `json:"assertion"`
					}
					if err := json.Unmarshal(assertionJSON, &assertionData); err != nil {
						verifyErr = fmt.Errorf("invalid webauthn assertion JSON: %w", err)
					} else {
						// Re-encode for VerifyWebAuthnAssertion
						assertionResponse, _ := json.Marshal(assertionData.Assertion)
						var sessionIDPtr *int64
						if authUser.Session != nil {
							sessionIDPtr = &authUser.Session.ID
						}
						_, verifyErr = h.mfaService.VerifyWebAuthnAssertion(userRecord, []byte(assertionData.SessionData), assertionResponse, clientIPFromRequest(r), r.UserAgent(), sessionIDPtr)
					}
				}
			} else {
				verifyErr = fmt.Errorf("MFA service not available")
			}
		default:
			verifyErr = fmt.Errorf("unsupported MFA type: %s", authUser.MFAType)
		}

		if verifyErr != nil {
			h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusUnauthorized, time.Since(start), fmt.Errorf("MFA verification failed: %w", verifyErr))
			http.Error(w, "MFA verification failed", http.StatusUnauthorized)
			return verifyErr
		}

		// MFA verified - update recent step-up timestamp if this is a web session
		if h.sessionManager != nil && authUser.Session != nil {
			now := time.Now()
			if err := h.sessionManager.UpdateSessionRecentStepUp(authUser.Session.ID, now); err != nil {
				log.Printf("Warning: failed to update session step-up: %v", err)
			}
		}

		return nil
	}

	// No inline MFA provided - check for session with recent step-up
	if authUser.Session == nil {
		h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusUnauthorized, time.Since(start), fmt.Errorf("session required for MFA verification"))
		http.Error(w, "session required for MFA verification", http.StatusUnauthorized)
		return fmt.Errorf("session required")
	}

	// No inline MFA provided - check if step-up window is still valid
	if authUser.Session.RecentStepUpAt != nil {
		// Get step-up window duration from settings
		stepUpWindow := 10 * time.Minute // Default
		if h.settingsService != nil {
			if settings, err := h.settingsService.GetMFASettings(); err == nil && settings != nil && settings.StepUpWindowMinutes > 0 {
				stepUpWindow = time.Duration(settings.StepUpWindowMinutes) * time.Minute
			}
		}

		if time.Since(*authUser.Session.RecentStepUpAt) < stepUpWindow {
			// Still within step-up window
			return nil
		}
	}

	// MFA required but not provided or expired
	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", http.StatusUnauthorized, time.Since(start), fmt.Errorf("MFA required for this action"))
	w.Header().Set("X-MFA-Required", "true")
	w.Header().Set("X-MFA-Level", mfaLevel.String())
	http.Error(w, "Multi-factor authentication is required for this action", http.StatusUnauthorized)
	return fmt.Errorf("MFA required")
}
