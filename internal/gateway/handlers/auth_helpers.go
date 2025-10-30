package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

const (
	webOAuthStateCookie       = "rgw_oauth_state"
	webOAuthStateTTL          = 5 * time.Minute
	trustedDeviceCookie       = "rgw_trusted_device"
	cliEnrollmentErrorMessage = "You must set up multi-factor authentication before you can continue using the CLI."
)

func extractSessionToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if cookie, err := c.Cookie("session_token"); err == nil {
		if trimmed := strings.TrimSpace(cookie); trimmed != "" {
			return trimmed
		}
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if len(authHeader) >= len(bearerPrefix) && strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return strings.TrimSpace(authHeader[len(bearerPrefix):])
		}
	}
	return ""
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, value string) {
	secure := h.cookieSecure()
	maxAge := 0
	if ttl := h.sessionsTTL(); ttl > 0 {
		// Keep the cookie as a session cookie to avoid forcing logouts while active.
		// Sliding expiration is enforced server-side via the session manager.
		maxAge = 0
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"session_token",
		value,
		maxAge,
		"/",
		"",
		secure,
		true,
	)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) sessionsTTL() time.Duration {
	if h.sessions == nil {
		return 30 * 24 * time.Hour
	}
	return h.sessions.TTL()
}

func (h *AuthHandler) setWebOAuthStateCookie(c *gin.Context, value string) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	maxAge := int((webOAuthStateTTL) / time.Second)
	c.SetCookie(webOAuthStateCookie, value, maxAge, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) clearWebOAuthStateCookie(c *gin.Context) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(webOAuthStateCookie, "", -1, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) cookieSecure() bool {
	defaultSecure := h == nil || h.config == nil || !h.config.DevMode
	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return defaultSecure
}

// Helper to audit login attempts
func (h *AuthHandler) auditLogin(c *gin.Context, resource, status string) {
	if h.database == nil {
		return
	}

	if err := h.auditLogger.LogDBEntry(&db.AuditLog{
		ActionType:   "auth",
		Action:       audit.BuildAction(audit.ActionScopeLogin, rbac.ActionStart.String()),
		ResourceType: "auth",
		Resource:     resource,
		Status:       status,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	}); err != nil {
		log.Printf(`{"level":"error","event":"audit_log_failed","action":audit.BuildAction(audit.ActionScopeLogin, rbac.ActionStart.String()),"error":%q}`, err)
	}
}

// createLoginSession creates a session after OAuth completion and handles post-login MFA checks
func (h *AuthHandler) createLoginSession(c *gin.Context, userRecord *db.User, loginFlow string) (*db.UserSession, error) {
	if h.sessions == nil {
		return nil, fmt.Errorf("session manager not available")
	}

	sessionToken, session, err := h.sessions.CreateSession(userRecord, auth.SessionMetadata{
		Channel:   "web",
		IPAddress: c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Extra: map[string]interface{}{
			"login_flow": loginFlow,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Try to mark as MFA verified if trusted device exists
	if err := h.handlePostLoginMFA(c, userRecord, session); err != nil {
		log.Printf("post-login mfa failed: user=%s session=%d flow=%s err=%v", userRecord.Email, session.ID, loginFlow, err)
	}

	h.setSessionCookie(c, sessionToken)
	log.Printf("Created session: user=%s session=%d flow=%s", userRecord.Email, session.ID, loginFlow)
	return session, nil
}

func (h *AuthHandler) handlePostLoginMFA(c *gin.Context, user *db.User, session *db.UserSession) error {
	if h.sessions == nil || user == nil || session == nil {
		return nil
	}

	if !h.isMFARequired(user) {
		now := time.Now()
		if err := h.sessions.UpdateSessionMFAVerified(session.ID, now, nil); err != nil {
			return fmt.Errorf("mark session verified: %w", err)
		}
		session.MFAVerifiedAt = &now
		return nil
	}

	trustedDevice, err := h.consumeTrustedDevice(c, user)
	if err != nil {
		log.Printf("post-login mfa: consume trusted device failed for user=%s: %v", user.Email, err)
		h.clearTrustedDeviceCookie(c)
		return nil
	}
	if trustedDevice == nil {
		return nil
	}

	now := time.Now()
	if err := h.sessions.UpdateSessionMFAVerified(session.ID, now, &trustedDevice.ID); err != nil {
		return fmt.Errorf("update session with trusted device: %w", err)
	}
	session.MFAVerifiedAt = &now
	session.TrustedDeviceID = &trustedDevice.ID
	return nil
}

func (h *AuthHandler) consumeTrustedDevice(c *gin.Context, user *db.User) (*db.TrustedDevice, error) {
	if h.mfaService == nil || user == nil {
		return nil, nil
	}

	cookie, err := c.Request.Cookie(trustedDeviceCookie)
	if err != nil {
		return nil, nil
	}
	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return nil, nil
	}

	device, err := h.mfaService.ConsumeTrustedDevice(token, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.clearTrustedDeviceCookie(c)
		return nil, err
	}
	if device.UserID != user.ID {
		_ = h.database.RevokeTrustedDevice(device.ID, "mismatched_user")
		h.clearTrustedDeviceCookie(c)
		return nil, fmt.Errorf("trusted device mismatch")
	}

	return device, nil
}

func (h *AuthHandler) setTrustedDeviceCookie(c *gin.Context, token string) {
	secure := h.cookieSecure()
	maxAge := h.trustedDeviceMaxAge()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(trustedDeviceCookie, token, maxAge, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) clearTrustedDeviceCookie(c *gin.Context) {
	secure := h.cookieSecure()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(trustedDeviceCookie, "", -1, "/", "", secure, true)
	c.SetSameSite(http.SameSiteDefaultMode)
}

func (h *AuthHandler) isMFARequired(user *db.User) bool {
	return isMFAChallengeRequired(h.mfaSettings, user)
}

func (h *AuthHandler) stepUpWindow() time.Duration {
	window := 10 * time.Minute
	if h.mfaSettings != nil && h.mfaSettings.StepUpWindowMinutes > 0 {
		window = time.Duration(h.mfaSettings.StepUpWindowMinutes) * time.Minute
	}
	return window
}

func (h *AuthHandler) trustedDeviceMaxAge() int {
	ttl := 30 * 24 * time.Hour
	if h.mfaSettings != nil && h.mfaSettings.TrustedDeviceTTLDays > 0 {
		ttl = time.Duration(h.mfaSettings.TrustedDeviceTTLDays) * 24 * time.Hour
	}
	return int(ttl.Seconds())
}

func makeMFAMethodResponse(method *db.MFAMethod) MFAMethodResponse {
	if method == nil {
		return MFAMethodResponse{}
	}
	label := strings.TrimSpace(method.Label)
	if label == "" {
		label = strings.ToUpper(strings.TrimSpace(method.Type))
	}
	resp := MFAMethodResponse{
		ID:        method.ID,
		Type:      method.Type,
		Label:     truncateLabel(label, 120),
		CreatedAt: method.CreatedAt,
	}
	if method.ConfirmedAt != nil {
		confirmed := *method.ConfirmedAt
		resp.ConfirmedAt = &confirmed
	}
	if method.LastUsedAt != nil {
		last := *method.LastUsedAt
		resp.LastUsedAt = &last
	}
	return resp
}

func makeTrustedDeviceResponse(device *db.TrustedDevice) TrustedDeviceResponse {
	if device == nil {
		return TrustedDeviceResponse{}
	}
	ua := extractTrustedDeviceUserAgent(device)
	label := ua
	if label == "" {
		label = fmt.Sprintf("Device %s", shortDeviceID(device.DeviceID))
	}
	ip := strings.TrimSpace(device.IPLast)
	if ip == "" {
		ip = strings.TrimSpace(device.IPFirst)
	}
	resp := TrustedDeviceResponse{
		ID:        device.ID,
		Label:     truncateLabel(label, 160),
		CreatedAt: device.CreatedAt,
		ExpiresAt: device.ExpiresAt,
		IPAddress: ip,
		UserAgent: truncateLabel(ua, 200),
	}
	last := device.LastUsedAt
	resp.LastUsedAt = &last
	if device.RevokedAt != nil {
		resp.RevokedAt = device.RevokedAt
	}
	if reason := strings.TrimSpace(device.RevokedReason); reason != "" {
		resp.RevokedReason = reason
	}
	return resp
}

func extractTrustedDeviceUserAgent(device *db.TrustedDevice) string {
	if device == nil || len(device.Metadata) == 0 {
		return ""
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(device.Metadata, &meta); err != nil {
		return ""
	}
	if ua, ok := meta["user_agent"].(string); ok {
		return strings.TrimSpace(ua)
	}
	return ""
}

func shortDeviceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		panic("CRITICAL: Empty device ID")
	}
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncateLabel(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func summarizeBackupCodes(codes []*db.MFABackupCode) MFABackupCodesSummary {
	summary := MFABackupCodesSummary{Total: len(codes)}
	for _, code := range codes {
		if code == nil {
			continue
		}
		created := code.CreatedAt
		if summary.LastGeneratedAt == nil || created.After(*summary.LastGeneratedAt) {
			createdCopy := created
			summary.LastGeneratedAt = &createdCopy
		}
		if code.UsedAt == nil {
			summary.Unused++
			continue
		}
		used := *code.UsedAt
		if summary.LastUsedAt == nil || used.After(*summary.LastUsedAt) {
			usedCopy := used
			summary.LastUsedAt = &usedCopy
		}
	}
	return summary
}

// handleMFADisablement checks if a user has any confirmed MFA methods remaining,
// and if not, marks the user as not enrolled and revokes all trusted devices.
func (h *AuthHandler) handleMFADisablement(userID int64) {
	remaining, err := h.database.ListMFAMethods(userID)
	if err != nil {
		log.Printf("failed to list remaining mfa methods: %v", err)
		return
	}

	hasConfirmed := false
	for _, candidate := range remaining {
		if candidate != nil && candidate.ConfirmedAt != nil {
			hasConfirmed = true
			break
		}
	}

	if !hasConfirmed {
		if err := h.database.SetUserMFAEnrolled(userID, false); err != nil {
			log.Printf("failed to update mfa enrollment after delete: %v", err)
		}

		trustedDevices, err := h.database.ListTrustedDevices(userID)
		if err != nil {
			log.Printf("failed to list trusted devices: %v", err)
			return
		}

		for _, device := range trustedDevices {
			if device != nil && device.RevokedAt == nil {
				if err := h.database.RevokeTrustedDevice(device.ID, "mfa_disabled"); err != nil {
					log.Printf("failed to revoke trusted device %d: %v", device.ID, err)
				}
			}
		}
	}
}

// auditMFAUpdate logs an MFA method update to the audit log
func (h *AuthHandler) auditMFAUpdate(c *gin.Context, user *db.User, methodID int64, label string) {
	if h.database == nil {
		return
	}

	details, _ := json.Marshal(map[string]interface{}{
		"method_id": methodID,
		"label":     label,
	})

	if err := h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    user.Email,
		UserName:     user.Name,
		ActionType:   "auth",
		Action:       audit.BuildAction(audit.ActionScopeMFAPreferences, rbac.ActionUpdate.String()),
		ResourceType: "mfa_method",
		Resource:     fmt.Sprintf("%d", methodID),
		Details:      string(details),
		Status:       "success",
		IPAddress:    c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	}); err != nil {
		log.Printf("failed to log mfa update audit: %v", err)
	}
}

// requireAuth checks authentication and authorization for a given resource and action.
// Returns the authenticated user's email and true if authorized, otherwise writes error response and returns false.
func requireAuth(c *gin.Context, rbacSvc rbac.RBACManager, resource rbac.Resource, action rbac.Action) (string, bool) {
	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return "", false
	}

	allowed, err := rbacSvc.Enforce(userEmail, rbac.ScopeGateway, resource, action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return "", false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return "", false
	}

	return userEmail, true
}
