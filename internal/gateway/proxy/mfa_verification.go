package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// verifyMFAIfRequired checks the MFA the route's permission needs. A step-up permission is covered by a step-up
// verified in the last few minutes (the step-up window), so a CLI command that makes several requests (env set
// --promote, a deploy) needs one MFA proof: the first request verifies it and the rest reuse the step-up. An
// MFAAlways permission needs a proof verified on the request itself, whatever the step-up window says; the CLI sends
// one with every MFAAlways command. The MFA settings are read for each request, so a change applies at once.
func (h *Handler) verifyMFAIfRequired(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	resource rbac.Resource,
	action rbac.Action,
	rackConfig *config.RackConfig,
	start time.Time,
) error {
	settings, err := h.mfaSettings()
	if err != nil {
		h.logMFADenial(
			r, w, authUser, rackConfig, start, "failed to load MFA settings", http.StatusInternalServerError, err,
		)
		return err
	}
	if sessionAwaitingMFA(authUser, settings) {
		w.Header().Set("X-MFA-Required", "true")
		h.logMFADenial(
			r, w, authUser, rackConfig, start,
			"Multi-factor authentication must be completed for this session",
			http.StatusUnauthorized,
			fmt.Errorf("session has not completed MFA"),
		)
		return fmt.Errorf("session has not completed MFA")
	}

	if h.mfaService == nil || h.sessionManager == nil {
		return nil
	}

	mfaLevel := determineMFALevel(resource, action)
	if mfaLevel == rbac.MFANone {
		return nil
	}
	if authUser.Session == nil {
		err := fmt.Errorf("session required for MFA verification")
		h.logMFADenial(r, w, authUser, rackConfig, start, err.Error(), http.StatusUnauthorized, err)
		return err
	}
	if mfaLevel == rbac.MFAStepUp && isStepUpValid(authUser, settings) {
		return nil
	}
	return h.requireRequestMFA(r, w, authUser, rackConfig, mfaLevel, start)
}

// mfaSettings loads the MFA settings as they are now. Without a settings service they're nil, which enforces MFA
// with the default windows.
func (h *Handler) mfaSettings() (*db.MFASettings, error) {
	if h.settingsService == nil {
		return nil, nil
	}
	settings, err := h.settingsService.GetMFASettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load MFA settings: %w", err)
	}
	return settings, nil
}

// sessionAwaitingMFA reports whether a session token is being used before its MFA challenge
// was completed (e.g. a web session cookie value replayed as a Bearer token on the CLI proxy).
func sessionAwaitingMFA(authUser *auth.User, settings *db.MFASettings) bool {
	if authUser == nil || authUser.IsAPIToken || authUser.Session == nil {
		return false
	}
	return db.SessionAwaitingMFA(settings, authUser.DBUser, authUser.Session)
}

// determineMFALevel returns the MFA level of a rack route's permission. Like the gateway's own routes and the CLI,
// it fails closed for a write permission missing from rbac.MFARequirements.
func determineMFALevel(resource rbac.Resource, action rbac.Action) rbac.MFALevel {
	return rbac.MFALevelForPermission(rbac.Convox(resource, action))
}

// requireRequestMFA verifies the MFA proof the request carries, and refuses the request without one.
func (h *Handler) requireRequestMFA(
	r *http.Request,
	w http.ResponseWriter,
	authUser *auth.User,
	rackConfig *config.RackConfig,
	mfaLevel rbac.MFALevel,
	start time.Time,
) error {
	err := h.verifyRequestMFA(r, authUser)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errNoMFAProof):
		w.Header().Set("X-MFA-Required", "true")
		w.Header().Set("X-MFA-Level", mfaLevel.String())
		h.logMFADenial(
			r, w, authUser, rackConfig, start,
			"Multi-factor authentication is required for this action",
			http.StatusUnauthorized,
			fmt.Errorf("MFA required for this action"),
		)
	case errors.Is(err, errMFAUnavailable):
		h.logMFADenial(r, w, authUser, rackConfig, start, "failed to verify MFA", http.StatusInternalServerError, err)
	default:
		h.logMFADenial(
			r, w, authUser, rackConfig, start,
			"MFA verification failed",
			http.StatusUnauthorized,
			fmt.Errorf("MFA verification failed: %w", err),
		)
	}
	return err
}

var (
	// errNoMFAProof is a request that must prove MFA itself but carries no MFA proof.
	errNoMFAProof = errors.New("no MFA proof on the request")
	// errMFAUnavailable is an MFA proof the gateway can't check (no MFA service, or the user can't be loaded).
	errMFAUnavailable = errors.New("MFA verification is unavailable")
)

// verifyRequestMFA verifies the MFA proof the request carries, at most once per request: TOTP codes and WebAuthn
// challenges are single-use, so a second check of the same proof would fail. A verified proof starts a new step-up
// window, which covers the rest of the command, once the request's audit attempt is written (ProxyToRack).
func (h *Handler) verifyRequestMFA(r *http.Request, authUser *auth.User) error {
	if inlineMFAVerified(r) {
		return nil
	}
	if authUser.MFAType == "" || authUser.MFAValue == "" {
		return errNoMFAProof
	}
	if h.mfaService == nil || h.database == nil {
		return errMFAUnavailable
	}
	userRecord, err := h.database.GetUser(authUser.Email)
	if err != nil {
		return fmt.Errorf("%w: %w", errMFAUnavailable, err)
	}
	if err := h.verifyMFAByType(r, authUser, userRecord); err != nil {
		return err
	}
	markInlineMFAVerified(r)
	return nil
}

type inlineMFAContextKeyType string

// inlineMFAVerifiedContextKey marks a request whose inline MFA proof has been verified, so later checks on the
// same request don't verify the single-use proof again (TOTP replay protection would reject it).
const inlineMFAVerifiedContextKey inlineMFAContextKeyType = "inlineMFAVerified"

func markInlineMFAVerified(r *http.Request) {
	*r = *r.WithContext(context.WithValue(r.Context(), inlineMFAVerifiedContextKey, true))
}

func inlineMFAVerified(r *http.Request) bool {
	verified, _ := r.Context().Value(inlineMFAVerifiedContextKey).(bool)
	return verified
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
		netutil.ClientIP(r),
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

	sessionIDPtr := getSessionIDPtr(authUser)
	_, err = h.mfaService.VerifyWebAuthnAssertion(
		userRecord,
		[]byte(assertionData.SessionData),
		[]byte(assertionData.AssertionResponse),
		netutil.ClientIP(r),
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
	if data.SessionData == "" {
		return nil, fmt.Errorf("invalid webauthn assertion JSON: missing session_data")
	}
	if data.AssertionResponse == "" {
		return nil, fmt.Errorf("invalid webauthn assertion JSON: missing assertion_response")
	}
	return &data, nil
}

type webAuthnAssertionData struct {
	SessionData       string `json:"session_data"`
	AssertionResponse string `json:"assertion_response"`
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

// isStepUpValid reports whether the session verified MFA within the step-up window.
func isStepUpValid(authUser *auth.User, settings *db.MFASettings) bool {
	recent := authUser.Session.RecentStepUpAt
	return recent != nil && time.Since(*recent) < settings.StepUpWindow()
}
