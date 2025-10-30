package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// verifyMFAIfRequired checks if MFA is required for the route and verifies inline MFA if provided
func (h *Handler) verifyMFAIfRequired(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	resource rbac.Resource,
	action rbac.Action,
	rackConfig *config.RackConfig,
	start time.Time,
) error {
	if h.mfaService == nil || h.sessionManager == nil {
		return nil
	}

	mfaLevel := determineMFALevel(resource, action)
	if mfaLevel == rbac.MFANone {
		return nil
	}

	if authUser.MFAType != "" && authUser.MFAValue != "" {
		return h.verifyInlineMFA(r, w, authUser, rackConfig, start)
	}

	return h.checkSessionStepUp(r, w, authUser, rackConfig, mfaLevel, start)
}

func determineMFALevel(resource rbac.Resource, action rbac.Action) rbac.MFALevel {
	permission := fmt.Sprintf("convox:%s:%s", resource.String(), action.String())
	mfaLevel, ok := rbac.MFARequirements[permission]
	if !ok {
		return rbac.MFANone
	}
	return mfaLevel
}

func (h *Handler) verifyInlineMFA(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	rackConfig *config.RackConfig,
	start time.Time,
) error {
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		h.logMFADenial(r, w, authUser, rackConfig, start, "failed to verify MFA", http.StatusInternalServerError, err)
		return fmt.Errorf("failed to get user")
	}

	verifyErr := h.verifyMFAByType(r, authUser, userRecord)
	if verifyErr != nil {
		h.logMFADenial(
			r, w, authUser, rackConfig, start,
			"MFA verification failed",
			http.StatusUnauthorized,
			fmt.Errorf("MFA verification failed: %w", verifyErr),
		)
		return verifyErr
	}

	h.updateSessionStepUp(authUser)
	return nil
}

func (h *Handler) verifyMFAByType(r *http.Request, authUser *auth.User, userRecord *db.User) error {
	switch authUser.MFAType {
	case "totp":
		return h.verifyTOTP(r, authUser, userRecord)
	case "webauthn":
		return h.verifyWebAuthn(r, authUser, userRecord)
	default:
		return fmt.Errorf("unsupported MFA type: %s", authUser.MFAType)
	}
}

func (h *Handler) verifyTOTP(r *http.Request, authUser *auth.User, userRecord *db.User) error {
	sessionIDPtr := getSessionIDPtr(authUser)
	result, err := h.mfaService.VerifyTOTP(
		userRecord,
		authUser.MFAValue,
		clientIPFromRequest(r),
		r.UserAgent(),
		sessionIDPtr,
	)
	if err == nil && result != nil {
		log.Printf("MFA verification successful: method_id=%d", result.MethodID)
	}
	return err
}

func (h *Handler) verifyWebAuthn(r *http.Request, authUser *auth.User, userRecord *db.User) error {
	assertionJSON, err := base64.StdEncoding.DecodeString(authUser.MFAValue)
	if err != nil {
		return fmt.Errorf("invalid webauthn assertion format: %w", err)
	}

	assertionData, err := parseWebAuthnAssertion(assertionJSON)
	if err != nil {
		return err
	}

	assertionResponse, _ := json.Marshal(assertionData.Assertion)
	sessionIDPtr := getSessionIDPtr(authUser)
	_, err = h.mfaService.VerifyWebAuthnAssertion(
		userRecord,
		[]byte(assertionData.SessionData),
		assertionResponse,
		clientIPFromRequest(r),
		r.UserAgent(),
		sessionIDPtr,
	)
	return err
}

func parseWebAuthnAssertion(assertionJSON []byte) (*webAuthnAssertionData, error) {
	var data webAuthnAssertionData
	if err := json.Unmarshal(assertionJSON, &data); err != nil {
		return nil, fmt.Errorf("invalid webauthn assertion JSON: %w", err)
	}
	return &data, nil
}

type webAuthnAssertionData struct {
	SessionData string `json:"session_data"`
	Assertion   struct {
		CredentialID      string `json:"credential_id"`
		AuthenticatorData string `json:"authenticator_data"`
		ClientDataJSON    string `json:"client_data_json"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"user_handle"`
	} `json:"assertion"`
}

func getSessionIDPtr(authUser *auth.User) *int64 {
	if authUser.Session != nil {
		return &authUser.Session.ID
	}
	return nil
}

func (h *Handler) updateSessionStepUp(authUser *auth.User) {
	if h.sessionManager != nil && authUser.Session != nil {
		now := time.Now()
		if err := h.sessionManager.UpdateSessionRecentStepUp(authUser.Session.ID, now); err != nil {
			log.Printf("Warning: failed to update session step-up: %v", err)
		}
	}
}

func (h *Handler) logMFADenial(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	rackConfig *config.RackConfig,
	start time.Time,
	message string,
	status int,
	err error,
) {
	h.auditLogger.LogRequest(r, authUser.Email, rackConfig.Name, "deny", status, time.Since(start), err)
	http.Error(w, message, status)
}

func (h *Handler) checkSessionStepUp(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	rackConfig *config.RackConfig,
	mfaLevel rbac.MFALevel,
	start time.Time,
) error {
	if authUser.Session == nil {
		h.logMFADenial(
			r, w, authUser, rackConfig, start,
			"session required for MFA verification",
			http.StatusUnauthorized,
			fmt.Errorf("session required for MFA verification"),
		)
		return fmt.Errorf("session required")
	}

	if h.isStepUpValid(authUser) {
		return nil
	}

	h.auditLogger.LogRequest(
		r, authUser.Email, rackConfig.Name, "deny",
		http.StatusUnauthorized, time.Since(start),
		fmt.Errorf("MFA required for this action"),
	)
	w.Header().Set("X-MFA-Required", "true")
	w.Header().Set("X-MFA-Level", mfaLevel.String())
	http.Error(w, "Multi-factor authentication is required for this action", http.StatusUnauthorized)
	return fmt.Errorf("MFA required")
}

func (h *Handler) isStepUpValid(authUser *auth.User) bool {
	if authUser.Session.RecentStepUpAt == nil {
		return false
	}
	stepUpWindow := h.getStepUpWindow()
	return time.Since(*authUser.Session.RecentStepUpAt) < stepUpWindow
}

func (h *Handler) getStepUpWindow() time.Duration {
	defaultWindow := 10 * time.Minute
	if h.settingsService == nil {
		return defaultWindow
	}
	settings, err := h.settingsService.GetMFASettings()
	if err != nil || settings == nil || settings.StepUpWindowMinutes <= 0 {
		return defaultWindow
	}
	return time.Duration(settings.StepUpWindowMinutes) * time.Minute
}
