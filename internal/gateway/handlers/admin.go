package handlers

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/routematch"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
	sentry "github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// AdminHandler handles admin API endpoints
type AdminHandler struct {
	rbac         rbac.RBACManager
	database     *db.Database
	tokenService *token.Service
	emailSender  email.Sender
	config       *config.Config
	rackCertMgr  *rackcert.Manager
	sessions     *auth.SessionManager
	mfaSettings  *db.MFASettings
	auditLogger  *audit.Logger
}

func cloneDetails(details map[string]interface{}) map[string]interface{} {
	if len(details) == 0 {
		return nil
	}
	copy := make(map[string]interface{}, len(details))
	for k, v := range details {
		copy[k] = v
	}
	return copy
}

func (h *AdminHandler) rackDisplay() string {
	if h == nil || h.config == nil {
		return "Convox Rack"
	}
	preferred := []string{"default", "local"}
	for _, key := range preferred {
		if rc, ok := h.config.Racks[key]; ok && rc.Enabled {
			if alias := strings.TrimSpace(rc.Alias); alias != "" {
				return alias
			}
			if name := strings.TrimSpace(rc.Name); name != "" {
				return name
			}
		}
	}
	for _, rc := range h.config.Racks {
		if !rc.Enabled {
			continue
		}
		if alias := strings.TrimSpace(rc.Alias); alias != "" {
			return alias
		}
		if name := strings.TrimSpace(rc.Name); name != "" {
			return name
		}
	}
	return "Convox Rack"
}

func (h *AdminHandler) publicBaseURL(c *gin.Context) string {
	if h != nil && h.config != nil {
		if raw := strings.TrimSpace(h.config.Domain); raw != "" {
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				return raw
			}
			if strings.Contains(raw, "localhost") || strings.Contains(raw, ":") {
				return "http://" + raw
			}
			return "https://" + raw
		}
	}
	if c != nil && c.Request != nil {
		scheme := "https"
		if proto := strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = proto
		} else if c.Request.TLS == nil {
			scheme = "http"
		}
		host := strings.TrimSpace(c.Request.Host)
		if host != "" {
			return fmt.Sprintf("%s://%s", scheme, host)
		}
	}
	return ""
}

// TriggerSentryTest dispatches a synthetic event to validate Sentry plumbing.
func (h *AdminHandler) TriggerSentryTest(c *gin.Context) {
	var payload struct {
		Kind string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	kind := strings.TrimSpace(strings.ToLower(payload.Kind))
	if kind == "" {
		kind = "api"
	}

	switch kind {
	case "api":
		hub := sentrygin.GetHubFromContext(c)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}
		if hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("trigger", "admin-api")
				scope.SetTag("test", "sentry-api")
				scope.SetExtra("triggered_at", time.Now().UTC().Format(time.RFC3339))
				if user := h.currentAuthUser(c); user != nil {
					scope.SetUser(sentry.User{Email: user.Email, Username: user.Name})
				}
				hub.CaptureMessage("Sentry API test event requested via settings page")
			})
		}
		c.JSON(http.StatusOK, gin.H{"status": "captured"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported test kind"})
	}
}

func (h *AdminHandler) currentAuthUser(c *gin.Context) *auth.AuthUser {
	if c == nil || c.Request == nil {
		return nil
	}
	if user, ok := auth.GetAuthUser(c.Request.Context()); ok && user != nil {
		return user
	}
	email := strings.TrimSpace(c.GetString("user_email"))
	name := strings.TrimSpace(c.GetString("user_name"))
	if email == "" {
		return nil
	}
	return &auth.AuthUser{Email: email, Name: name}
}

func (h *AdminHandler) getAdminEmails() []string {
	if h == nil || h.rbac == nil {
		return nil
	}
	users, err := h.rbac.GetUsers()
	if err != nil {
		return nil
	}
	emails := make([]string, 0)
	for email, user := range users {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		for _, role := range user.Roles {
			if role == "admin" {
				emails = append(emails, email)
				break
			}
		}
	}
	if len(emails) == 0 {
		return nil
	}
	sort.Strings(emails)
	return emails
}

func prioritiseInviterFirst(admins []string, inviterEmail string) []string {
	if inviterEmail == "" || len(admins) == 0 {
		return admins
	}
	idx := -1
	for i, addr := range admins {
		if strings.EqualFold(addr, inviterEmail) {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return admins
	}
	reordered := make([]string, 0, len(admins))
	reordered = append(reordered, admins[idx])
	for i, addr := range admins {
		if i == idx {
			continue
		}
		reordered = append(reordered, addr)
	}
	return reordered
}

var (
	errInvalidStartTime = errors.New("invalid start time")
	errInvalidEndTime   = errors.New("invalid end time")
	errInvalidTimeRange = errors.New("end time must be after start time")
)

// NewAdminHandler creates a new admin handler
func NewAdminHandler(rbac rbac.RBACManager, database *db.Database, tokenService *token.Service, emailSender email.Sender, config *config.Config, rackCertMgr *rackcert.Manager, sessions *auth.SessionManager, mfaSettings *db.MFASettings, auditLogger *audit.Logger) *AdminHandler {
	return &AdminHandler{
		rbac:         rbac,
		database:     database,
		tokenService: tokenService,
		emailSender:  emailSender,
		config:       config,
		rackCertMgr:  rackCertMgr,
		sessions:     sessions,
		mfaSettings:  mfaSettings,
		auditLogger:  auditLogger,
	}
}

func (h *AdminHandler) auditAdminAction(c *gin.Context, action, resource, status string, httpStatus int, details map[string]interface{}, start time.Time) {
	if h == nil || h.database == nil {
		return
	}

	if action == "audit.list" {
		return
	}

	trimmedResource := strings.TrimSpace(resource)
	detailsCopy := cloneDetails(details)
	var detailsJSON string
	if len(detailsCopy) > 0 {
		if payload, err := json.Marshal(detailsCopy); err == nil {
			detailsJSON = string(payload)
		}
	}

	email := strings.TrimSpace(c.GetString("user_email"))
	name := strings.TrimSpace(c.GetString("user_name"))
	if au, ok := auth.GetAuthUser(c.Request.Context()); ok && au != nil {
		if e := strings.TrimSpace(au.Email); e != "" {
			email = e
		}
		if n := strings.TrimSpace(au.Name); n != "" {
			name = n
		}
	}

	actionType := "admin"
	switch {
	case strings.HasPrefix(action, "api_token."):
		actionType = "tokens"
	case strings.HasPrefix(action, "user."):
		actionType = "users"
	case strings.HasPrefix(action, "audit."):
		actionType = "admin"
	}

	resourceType := "admin"
	switch {
	case strings.HasPrefix(action, "api_token."):
		resourceType = "api_token"
	case strings.HasPrefix(action, "user."):
		resourceType = "user"
	case strings.HasPrefix(action, "audit."):
		resourceType = "admin"
	}

	entry := &db.AuditLog{
		UserEmail:      email,
		UserName:       name,
		ActionType:     actionType,
		Action:         action,
		Resource:       trimmedResource,
		ResourceType:   resourceType,
		Details:        detailsJSON,
		IPAddress:      c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
		Status:         status,
		HTTPStatus:     httpStatus,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
	}

	switch status {
	case "denied":
		entry.RBACDecision = "deny"
	case "success":
		entry.RBACDecision = "allow"
	}

	_ = h.auditLogger.LogDBEntry(entry)
}

func (h *AdminHandler) respondAudit(c *gin.Context, statusCode int, payload interface{}, action, resource, auditStatus string, start time.Time, details map[string]interface{}) {
	if payload == nil {
		c.Status(statusCode)
	} else {
		c.JSON(statusCode, payload)
	}
	h.auditAdminAction(c, action, resource, auditStatus, statusCode, details, start)
}

func (h *AdminHandler) respondAuditSuccess(c *gin.Context, statusCode int, payload interface{}, action, resource string, start time.Time, details map[string]interface{}) {
	h.respondAudit(c, statusCode, payload, action, resource, "success", start, details)
}

func (h *AdminHandler) respondAuditError(c *gin.Context, statusCode int, action, resource, message string, start time.Time, details map[string]interface{}) {
	det := cloneDetails(details)
	if det == nil {
		det = make(map[string]interface{})
	}
	if message != "" {
		det["error"] = message
	}
	auditStatus := "error"
	if statusCode == http.StatusForbidden || statusCode == http.StatusUnauthorized {
		auditStatus = "denied"
	}
	h.respondAudit(c, statusCode, gin.H{"error": message}, action, resource, auditStatus, start, det)
}

// ListUsers godoc
// @Summary List all gateway users
// @Description Returns every user configured in the gateway along with role assignments.
// @Tags Users
// @Produce json
// @Success 200 {array} db.User
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/users [get]
func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.database.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	c.JSON(http.StatusOK, users)
}

// GetUser godoc
// @Summary Get a user
// @Description Returns details for a single gateway user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} db.User
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/users/{email} [get]
func (h *AdminHandler) GetUser(c *gin.Context) {
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// CreateUser godoc
// @Summary Create a user
// @Description Creates a new gateway user with the provided roles.
// @Tags Users
// @Accept json
// @Produce json
// @Param request body CreateUserRequest true "User payload"
// @Success 201 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users [post]
func (h *AdminHandler) CreateUser(c *gin.Context) {
	start := time.Now()
	var req CreateUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "user.create", strings.TrimSpace(req.Email), err.Error(), start, nil)
		return
	}

	// Note: the cicd role is automation-only and intentionally excluded here.
	validRoles := []string{"viewer", "ops", "deployer", "admin"}
	for _, role := range req.Roles {
		matched := false
		for _, vr := range validRoles {
			if role == vr {
				matched = true
				break
			}
		}
		if !matched {
			h.respondAuditError(c, http.StatusBadRequest, "user.create", strings.TrimSpace(req.Email), fmt.Sprintf("invalid role: %s", role), start, nil)
			return
		}
	}

	userConfig := &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}

	if err := h.rbac.SaveUser(req.Email, userConfig); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			h.respondAuditError(c, http.StatusConflict, "user.create", strings.TrimSpace(req.Email), "user already exists", start, nil)
			return
		}
		h.respondAuditError(c, http.StatusInternalServerError, "user.create", strings.TrimSpace(req.Email), "failed to create user", start, nil)
		return
	}

	if h.database != nil && h.rbac != nil {
		if creatorEmail := strings.TrimSpace(c.GetString("user_email")); creatorEmail != "" {
			if creator, err := h.rbac.GetUserWithID(creatorEmail); err == nil && creator != nil {
				if newUser, err := h.database.GetUser(req.Email); err == nil && newUser != nil {
					_, _ = h.database.CreateUserResource(creator.ID, "user", newUser.Email)
				}
			}
		}
	}

	payload := UserSummary{
		Email:          req.Email,
		Name:           req.Name,
		Roles:          req.Roles,
		CreatedByEmail: strings.TrimSpace(c.GetString("user_email")),
	}

	resource := strings.TrimSpace(req.Email)
	if userWithID, err := h.rbac.GetUserWithID(req.Email); err == nil && userWithID != nil && userWithID.ID > 0 {
		resource = fmt.Sprintf("%d", userWithID.ID)
	}

	details := map[string]interface{}{"email": req.Email, "roles": req.Roles}
	if strings.TrimSpace(req.Name) != "" {
		details["name"] = req.Name
	}

	h.respondAuditSuccess(c, http.StatusCreated, payload, "user.create", resource, start, details)

	h.notifyUserCreated(c, req)
}

func (h *AdminHandler) notifyUserCreated(c *gin.Context, req CreateUserRequest) {
	if h == nil || h.emailSender == nil {
		return
	}
	inviter := h.currentAuthUser(c)
	inviterEmail := ""
	if inviter != nil {
		inviterEmail = strings.TrimSpace(inviter.Email)
	}
	if inviterEmail == "" {
		inviterEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	rack := h.rackDisplay()
	base := h.publicBaseURL(c)
	recipient := strings.TrimSpace(req.Email)
	if recipient != "" {
		subjectUser := fmt.Sprintf("Rack Gateway (%s): You've been granted access", rack)
		textUser, htmlUser, err := emailtemplates.RenderWelcome(rack, recipient, inviterEmail, base, base)
		if err != nil || (textUser == "" && htmlUser == "") {
			roles := strings.Join(req.Roles, ", ")
			textUser = fmt.Sprintf("You've been granted access to the Rack Gateway (%s) with roles: %s.", rack, roles)
		}
		_ = h.emailSender.Send(recipient, subjectUser, textUser, htmlUser)
	}

	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	filtered := make([]string, 0, len(admins))
	for _, addr := range admins {
		if strings.EqualFold(addr, req.Email) {
			continue
		}
		filtered = append(filtered, addr)
	}
	if len(filtered) == 0 {
		return
	}
	sort.Strings(filtered)
	recipients := prioritiseInviterFirst(filtered, inviterEmail)
	creator := inviterEmail
	if creator == "" {
		creator = "an administrator"
	}
	subjectAdmin := fmt.Sprintf("Rack Gateway (%s): %s added %s (%s)", rack, creator, req.Email, req.Name)
	textAdmin, htmlAdmin, err := emailtemplates.RenderUserAddedAdmin(rack, creator, req.Email, req.Name, req.Roles)
	if err != nil || (textAdmin == "" && htmlAdmin == "") {
		textAdmin = fmt.Sprintf("%s added new user %s (%s) with roles: %s.", creator, req.Email, req.Name, strings.Join(req.Roles, ", "))
	}
	_ = h.emailSender.SendMany(recipients, subjectAdmin, textAdmin, htmlAdmin)
}

func (h *AdminHandler) notifySettingsChanged(c *gin.Context, key, value string) {
	if h == nil || h.emailSender == nil {
		return
	}
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	inviter := h.currentAuthUser(c)
	actorEmail := ""
	if inviter != nil {
		actorEmail = strings.TrimSpace(inviter.Email)
	}
	if actorEmail == "" {
		actorEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	actorLabel := actorEmail
	if actorLabel == "" {
		actorLabel = "an administrator"
	}
	sort.Strings(admins)
	recipients := prioritiseInviterFirst(admins, actorEmail)
	rack := h.rackDisplay()
	subject := fmt.Sprintf("Rack Gateway (%s): %s changed the %s setting", rack, actorLabel, key)
	text, html, err := emailtemplates.RenderSettingsChanged(rack, actorLabel, key, value)
	if err != nil || (text == "" && html == "") {
		text = fmt.Sprintf("%s changed %s to %s.", actorLabel, key, value)
	}
	_ = h.emailSender.SendMany(recipients, subject, text, html)
}

// DeleteUser godoc
// @Summary Delete a user
// @Description Removes a gateway user and revokes all sessions they own.
// @Tags Users
// @Param email path string true "User email"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email} [delete]
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	start := time.Now()
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't delete yourself
	if email == currentUser {
		h.respondAuditError(c, http.StatusBadRequest, "user.delete", strings.TrimSpace(email), "cannot delete yourself", start, nil)
		return
	}

	if err := h.rbac.DeleteUser(email); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.delete", strings.TrimSpace(email), "failed to delete user", start, nil)
		return
	}

	h.respondAuditSuccess(c, http.StatusNoContent, nil, "user.delete", strings.TrimSpace(email), start, nil)
}

// UpdateUserProfile godoc
// @Summary Update user profile
// @Description Updates a user's display name and/or email.
// @Tags Users
// @Accept json
// @Produce json
// @Param email path string true "Current email"
// @Param request body UpdateUserProfileRequest true "User profile"
// @Success 200 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email} [put]
func (h *AdminHandler) UpdateUserProfile(c *gin.Context) {
	start := time.Now()
	originalEmail := strings.TrimSpace(c.Param("email"))
	currentEmail := originalEmail

	var req UpdateUserProfileRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "user.update", originalEmail, err.Error(), start, nil)
		return
	}

	userConfig, err := h.rbac.GetUser(originalEmail)
	if err != nil || userConfig == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.update", originalEmail, "user not found", start, nil)
		return
	}

	dbUser, err := h.database.GetUser(originalEmail)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.update", originalEmail, "failed to load user", start, nil)
		return
	}
	if dbUser == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.update", originalEmail, "user not found", start, nil)
		return
	}

	updatedEmail := strings.TrimSpace(req.Email)
	if updatedEmail == "" {
		updatedEmail = currentEmail
	}

	emailChanged := !strings.EqualFold(updatedEmail, currentEmail)
	if emailChanged {
		if existing, err := h.database.GetUser(updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, "user.update", originalEmail, "failed to check email availability", start, nil)
			return
		} else if existing != nil {
			h.respondAuditError(c, http.StatusConflict, "user.update", originalEmail, "email already in use", start, map[string]interface{}{"email": updatedEmail})
			return
		}
		if err := h.database.UpdateUserEmail(originalEmail, updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, "user.update", originalEmail, "failed to update email", start, map[string]interface{}{"email": updatedEmail})
			return
		}
		currentEmail = updatedEmail
	}

	updatedName := strings.TrimSpace(req.Name)
	if updatedName == "" {
		updatedName = userConfig.Name
	}
	userConfig.Name = updatedName

	if err := h.rbac.SaveUser(currentEmail, userConfig); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.update", currentEmail, "failed to update user", start, map[string]interface{}{"email": currentEmail})
		return
	}

	payload := UserSummary{
		Email: currentEmail,
		Name:  updatedName,
		Roles: userConfig.Roles,
	}
	details := map[string]interface{}{"name": updatedName}
	if emailChanged {
		details["email"] = currentEmail
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, "user.update", currentEmail, start, details)
}

// UpdateUserRoles godoc
// @Summary Update user roles
// @Description Replaces the role assignments for a user.
// @Tags Users
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Param request body UpdateUserRolesRequest true "Role payload"
// @Success 200 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email}/roles [put]
func (h *AdminHandler) UpdateUserRoles(c *gin.Context) {
	start := time.Now()
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't change your own roles
	if email == currentUser {
		h.respondAuditError(c, http.StatusBadRequest, "user.update_roles", strings.TrimSpace(email), "cannot change your own roles", start, nil)
		return
	}

	var req UpdateUserRolesRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "user.update_roles", strings.TrimSpace(email), err.Error(), start, nil)
		return
	}

	user, err := h.rbac.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusNotFound, "user.update_roles", strings.TrimSpace(email), "user not found", start, nil)
		return
	}

	user.Roles = req.Roles
	if err := h.rbac.SaveUser(email, user); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.update_roles", strings.TrimSpace(email), "failed to update roles", start, nil)
		return
	}

	payload := UserSummary{
		Email: email,
		Name:  user.Name,
		Roles: req.Roles,
	}
	details := map[string]interface{}{"roles": req.Roles}
	h.respondAuditSuccess(c, http.StatusOK, payload, "user.update_roles", strings.TrimSpace(email), start, details)
}

// ListUserSessions godoc
// @Summary List active sessions for a user
// @Description Returns the active (non-revoked) web sessions for the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Success 200 {array} UserSessionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/users/{email}/sessions [get]
func (h *AdminHandler) ListUserSessions(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.list", email, "email is required", start, nil)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.list", email, "session management unavailable", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.list", email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.list", email, "user not found", start, nil)
		return
	}

	sessions, err := h.database.ListActiveSessionsByUser(user.ID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.list", email, "failed to list sessions", start, nil)
		return
	}

	result := make([]UserSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		entry := UserSessionResponse{
			ID:        sess.ID,
			CreatedAt: sess.CreatedAt.UTC().Format(time.RFC3339),
			LastSeen:  sess.LastSeenAt.UTC().Format(time.RFC3339),
			ExpiresAt: sess.ExpiresAt.UTC().Format(time.RFC3339),
			Channel:   sess.Channel,
		}
		if sess.IPAddress != "" {
			entry.IPAddress = sess.IPAddress
		}
		if sess.UserAgent != "" {
			entry.UserAgent = sess.UserAgent
		}
		if len(sess.Metadata) > 0 {
			var meta interface{}
			if err := json.Unmarshal(sess.Metadata, &meta); err == nil {
				entry.Metadata = meta
			} else {
				entry.Metadata = json.RawMessage(sess.Metadata)
			}
		}
		result = append(result, entry)
	}

	details := map[string]interface{}{"session_count": len(result)}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.list", email, start, details)
}

// RevokeUserSession godoc
// @Summary Revoke a user session
// @Description Revokes a single session for the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Param sessionID path int true "Session ID"
// @Success 200 {object} RevokeSessionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email}/sessions/{sessionID}/revoke [post]
func (h *AdminHandler) RevokeUserSession(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.revoke", email, "email is required", start, nil)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke", email, "session management unavailable", start, nil)
		return
	}

	sessionIDStr := strings.TrimSpace(c.Param("sessionID"))
	sessionID, err := strconv.ParseInt(sessionIDStr, 10, 64)
	if err != nil || sessionID <= 0 {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.revoke", email, "invalid session id", start, map[string]interface{}{"session_id": sessionIDStr})
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke", email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.revoke", email, "user not found", start, map[string]interface{}{"session_id": sessionID})
		return
	}

	session, err := h.database.GetUserSessionByID(sessionID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke", email, "failed to load session", start, map[string]interface{}{"session_id": sessionID})
		return
	}
	if session == nil || session.UserID != user.ID {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.revoke", email, "session not found", start, map[string]interface{}{"session_id": sessionID})
		return
	}

	actorID := h.sessionActorID(c)
	revoked, err := h.sessions.RevokeByID(sessionID, actorID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke", email, "failed to revoke session", start, map[string]interface{}{"session_id": sessionID})
		return
	}

	result := RevokeSessionResponse{Revoked: revoked}
	details := map[string]interface{}{"session_id": sessionID, "revoked": revoked}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.revoke", email, start, details)
}

// RevokeAllUserSessions godoc
// @Summary Revoke all sessions for a user
// @Description Revokes every active session belonging to the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} RevokeAllSessionsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email}/sessions/revoke_all [post]
func (h *AdminHandler) RevokeAllUserSessions(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.revoke_all", email, "email is required", start, nil)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke_all", email, "session management unavailable", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke_all", email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.revoke_all", email, "user not found", start, nil)
		return
	}

	actorID := h.sessionActorID(c)
	revokedCount, err := h.sessions.RevokeAllForUser(user.ID, actorID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.sessions.revoke_all", email, "failed to revoke sessions", start, nil)
		return
	}

	result := RevokeAllSessionsResponse{RevokedCount: revokedCount}
	details := map[string]interface{}{"revoked_count": revokedCount}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.revoke_all", email, start, details)
}

// LockUserRequest represents the payload for locking a user account
type LockUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// LockUser godoc
// @Summary Lock a user account
// @Description Locks a user account to prevent login
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Param request body LockUserRequest true "Lock reason"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email}/lock [post]
func (h *AdminHandler) LockUser(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.lock", email, "email is required", start, nil)
		return
	}

	var req LockUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "user.lock", email, "invalid request", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.lock", email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.lock", email, "user not found", start, nil)
		return
	}

	// Get current admin user
	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, "user.lock", email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.lock", email, "failed to load admin user", start, nil)
		return
	}

	// Lock the account
	if err := h.database.LockUser(user.ID, req.Reason, &adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.lock", email, "failed to lock user", start, nil)
		return
	}

	// Revoke all active sessions
	if h.sessions != nil {
		_, _ = h.sessions.RevokeAllForUser(user.ID, &adminUser.ID)
	}

	// Send email notification to user
	if h.emailSender != nil {
		subject := "Account Locked"
		textBody := fmt.Sprintf("Your account has been locked by an administrator.\n\nReason: %s\n\nPlease contact your administrator for assistance.", req.Reason)
		htmlBody := fmt.Sprintf("<p>Your account has been locked by an administrator.</p><p><strong>Reason:</strong> %s</p><p>Please contact your administrator for assistance.</p>", req.Reason)
		_ = h.emailSender.Send(user.Email, subject, textBody, htmlBody)
	}

	details := map[string]interface{}{
		"reason":      req.Reason,
		"locked_by":   adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user locked successfully"}, "user.lock", email, start, details)
}

// UnlockUser godoc
// @Summary Unlock a user account
// @Description Unlocks a previously locked user account
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/users/{email}/unlock [post]
func (h *AdminHandler) UnlockUser(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.unlock", email, "email is required", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.unlock", email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.unlock", email, "user not found", start, nil)
		return
	}

	// Get current admin user
	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, "user.unlock", email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.unlock", email, "failed to load admin user", start, nil)
		return
	}

	// Unlock the account
	if err := h.database.UnlockUser(user.ID, adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "user.unlock", email, "failed to unlock user", start, nil)
		return
	}

	// Send email notification to user
	if h.emailSender != nil {
		subject := "Account Unlocked"
		textBody := "Your account has been unlocked by an administrator.\n\nYou can now log in again."
		htmlBody := "<p>Your account has been unlocked by an administrator.</p><p>You can now log in again.</p>"
		_ = h.emailSender.Send(user.Email, subject, textBody, htmlBody)
	}

	details := map[string]interface{}{
		"unlocked_by": adminUser.Email,
		"target_user": user.Email,
	}
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user unlocked successfully"}, "user.unlock", email, start, details)
}

func (h *AdminHandler) sessionActorID(c *gin.Context) *int64 {
	if h == nil || h.database == nil {
		return nil
	}
	actorEmail := strings.TrimSpace(c.GetString("user_email"))
	if actorEmail == "" {
		return nil
	}
	actor, err := h.database.GetUser(actorEmail)
	if err != nil || actor == nil {
		return nil
	}
	id := actor.ID
	return &id
}

// ListRoles godoc
// @Summary List RBAC roles
// @Description Returns metadata and permissions for each gateway RBAC role.
// @Tags Roles
// @Produce json
// @Success 200 {object} map[string]RoleDescriptor
// @Security SessionCookie
// @Router /admin/roles [get]
func (h *AdminHandler) ListRoles(c *gin.Context) {
	rolePerms := rbac.DefaultRolePermissions()
	metaMap := rbac.RoleMetadataMap()

	roles := make(map[string]RoleDescriptor, len(metaMap))
	for _, role := range rbac.RoleOrder() {
		meta, ok := metaMap[role]
		if !ok {
			continue
		}
		perms := rolePerms[role]
		if role == "admin" {
			perms = []string{"convox:*:*"}
		}
		roles[role] = RoleDescriptor{
			Name:        role,
			Label:       meta.Label,
			Description: meta.Description,
			Permissions: perms,
		}
	}

	c.JSON(http.StatusOK, roles)
}

// ListAuditLogs godoc
// @Summary List audit logs
// @Description Returns paginated audit logs filtered by optional query parameters.
// @Tags Audit
// @Produce json
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param page query int false "Page number"
// @Param limit query int false "Page size"
// @Param start query string false "ISO8601 start time"
// @Param end query string false "ISO8601 end time"
// @Param range query string false "Relative range (e.g. 24h, 7d, custom)"
// @Param user query string false "Filter by user email"
// @Param user_id query string false "Filter by user ID"
// @Success 200 {object} AuditLogsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/audit [get]
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	start := time.Now()
	filters, page, limit, err := h.auditFiltersFromRequest(c)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidStartTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "invalid start time", start, nil)
		case errors.Is(err, errInvalidEndTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "invalid end time", start, nil)
		case errors.Is(err, errInvalidTimeRange):
			h.respondAuditError(c, http.StatusBadRequest, "audit.list", "", "end time must be after start time", start, nil)
		default:
			h.respondAuditError(c, http.StatusInternalServerError, "audit.list", "", "failed to fetch audit logs", start, nil)
		}
		return
	}

	logs, total, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.list", "", err.Error(), start, nil)
		return
	}

	eventTotal := 0
	for _, log := range logs {
		if log.EventCount > 0 {
			eventTotal += log.EventCount
		} else {
			eventTotal++
		}
	}

	payload := AuditLogsResponse{
		Logs:  logs,
		Total: total,
		Page:  page,
		Limit: limit,
	}

	details := map[string]interface{}{
		"total":         total,
		"event_total":   eventTotal,
		"page":          page,
		"limit":         limit,
		"action_type":   filters.ActionType,
		"status_filter": filters.Status,
		"resource_type": filters.ResourceType,
		"search":        filters.Search,
	}
	if !filters.Since.IsZero() {
		details["since"] = filters.Since.UTC().Format(time.RFC3339)
	}
	if !filters.Until.IsZero() {
		details["until"] = filters.Until.UTC().Format(time.RFC3339)
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, "audit.list", "", start, details)
}

// ExportAuditLogs godoc
// @Summary Export audit logs as CSV
// @Description Streams the filtered audit log dataset as CSV.
// @Tags Audit
// @Produce text/csv
// @Param search query string false "Text search"
// @Param action_type query string false "Action type filter"
// @Param resource_type query string false "Resource type filter"
// @Param status query string false "Status filter"
// @Param since query string false "ISO8601 start time"
// @Param until query string false "ISO8601 end time"
// @Success 200 {file} binary
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/audit/export [get]
func (h *AdminHandler) ExportAuditLogs(c *gin.Context) {
	start := time.Now()
	filters, _, _, err := h.auditFiltersFromRequest(c)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidStartTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "invalid start time", start, nil)
		case errors.Is(err, errInvalidEndTime):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "invalid end time", start, nil)
		case errors.Is(err, errInvalidTimeRange):
			h.respondAuditError(c, http.StatusBadRequest, "audit.export", "", "end time must be after start time", start, nil)
		default:
			h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to fetch logs", start, nil)
		}
		return
	}

	if filters.Limit <= 0 || filters.Limit > 10000 {
		filters.Limit = 10000
	}
	filters.Offset = 0

	logs, _, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", err.Error(), start, nil)
		return
	}

	// Set CSV headers
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-logs-%s.csv\"", time.Now().Format("2006-01-02")))

	// Write CSV
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Header row
	header := []string{
		"timestamp", "user_email", "user_name", "action_type", "action",
		"command", "resource", "status", "event_count", "response_time_ms", "ip_address", "user_agent",
	}
	if err := writer.Write(header); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to write CSV header", start, nil)
		return
	}

	// Data rows
	totalEvents := 0
	for _, log := range logs {
		count := log.EventCount
		if count <= 0 {
			count = 1
		}
		totalEvents += count
		row := []string{
			log.Timestamp.Format(time.RFC3339),
			log.UserEmail,
			log.UserName,
			log.ActionType,
			log.Action,
			log.Command,
			log.Resource,
			log.Status,
			strconv.Itoa(count),
			strconv.Itoa(log.ResponseTimeMs),
			log.IPAddress,
			log.UserAgent,
		}
		if err := writer.Write(row); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to write CSV row", start, nil)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "audit.export", "", "failed to flush CSV", start, nil)
		return
	}

	details := map[string]interface{}{
		"count":         len(logs),
		"events":        totalEvents,
		"action_type":   filters.ActionType,
		"status_filter": filters.Status,
		"resource_type": filters.ResourceType,
		"search":        filters.Search,
	}
	if !filters.Since.IsZero() {
		details["since"] = filters.Since.UTC().Format(time.RFC3339)
	}
	if !filters.Until.IsZero() {
		details["until"] = filters.Until.UTC().Format(time.RFC3339)
	}

	h.auditAdminAction(c, "audit.export", "", "success", http.StatusOK, details, start)
}

// Config and settings handlers

// GetConfig godoc
// @Summary Get legacy configuration
// @Description Returns the legacy user/domain configuration payload (deprecated).
// @Tags Config
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/config [get]
func (h *AdminHandler) GetConfig(c *gin.Context) {
	// Get users from the manager
	users, err := h.rbac.GetUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get users"})
		return
	}

	// Convert internal format to API format
	apiConfig := gin.H{
		"domain": h.rbac.GetAllowedDomain(),
		"users":  users,
	}

	c.JSON(http.StatusOK, apiConfig)
}

// UpdateConfig godoc
// @Summary Update legacy configuration
// @Description Placeholder endpoint retained for backwards compatibility. Always returns 501.
// @Tags Config
// @Accept json
// @Produce json
// @Failure 501 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/config [put]
func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	// Would update configuration
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// GetSettings godoc
// @Summary Get gateway admin settings
// @Description Returns administrative settings including protected env vars and rack TLS state.
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security SessionCookie
// @Router /admin/settings [get]
func (h *AdminHandler) GetSettings(c *gin.Context) {
	resp := make(map[string]interface{})

	if h.database != nil {
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			resp["protected_env_vars"] = arr
		} else {
			resp["protected_env_vars"] = []string{}
		}
		if v, err := h.database.GetAllowDestructiveActions(); err == nil {
			resp["allow_destructive_actions"] = v
		} else {
			resp["allow_destructive_actions"] = false
		}
		if arr, err := h.database.GetApprovedCommands(); err == nil {
			resp["approved_commands"] = arr
		} else {
			resp["approved_commands"] = []string{}
		}
		if settings, err := h.database.GetMFASettings(); err == nil && settings != nil {
			h.mfaSettings = settings
		}
	} else {
		resp["protected_env_vars"] = []string{}
		resp["allow_destructive_actions"] = false
		resp["approved_commands"] = []string{}
	}

	if h.mfaSettings != nil {
		resp["mfa"] = gin.H{
			"require_all_users":       h.mfaSettings.RequireAllUsers,
			"trusted_device_ttl_days": h.mfaSettings.TrustedDeviceTTLDays,
			"step_up_window_minutes":  h.mfaSettings.StepUpWindowMinutes,
		}
	}

	if h.config != nil {
		resp["sentry_tests_enabled"] = h.config.SentryTestsEnabled
	}

	pinningEnabled := h.config != nil && h.config.RackTLSPinningEnabled
	resp["rack_tls_pinning_enabled"] = pinningEnabled
	if pinningEnabled && h.rackCertMgr != nil {
		if cert, ok, err := h.rackCertMgr.CurrentCertificate(c.Request.Context()); err == nil && ok {
			resp["rack_tls_cert"] = gin.H{
				"pem":         cert.PEM,
				"fingerprint": cert.Fingerprint,
				"fetched_at":  cert.FetchedAt,
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateProtectedEnvVars godoc
// @Summary Update protected environment variables
// @Description Replaces the list of protected environment variable keys.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateProtectedEnvVarsRequest true "Protected env vars"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/protected_env_vars [put]
func (h *AdminHandler) UpdateProtectedEnvVars(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateProtectedEnvVarsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Normalize and de-dup to uppercase
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.ProtectedEnvVars))
	for _, k := range payload.ProtectedEnvVars {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("protected_env_vars", out, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		h.notifySettingsChanged(c, "protected_env_vars", strings.Join(out, ", "))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateApprovedCommands godoc
// @Summary Update approved commands for CI/CD exec
// @Description Replaces the list of approved commands that CI/CD tokens can execute in processes.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateApprovedCommandsRequest true "Approved commands"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/approved_commands [put]
func (h *AdminHandler) UpdateApprovedCommands(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateApprovedCommandsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Trim and de-dup
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.ApprovedCommands))
	for _, cmd := range payload.ApprovedCommands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		out = append(out, cmd)
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpdateApprovedCommands(out, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save approved commands"})
			return
		}

		h.notifySettingsChanged(c, "approved_commands", fmt.Sprintf("%d commands", len(out)))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateAllowDestructiveActions godoc
// @Summary Toggle destructive action protections
// @Description Enables or disables destructive actions such as rack resets.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateAllowDestructiveActionsRequest true "Toggle payload"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/allow_destructive_actions [put]
func (h *AdminHandler) UpdateAllowDestructiveActions(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateAllowDestructiveActionsRequest

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("allow_destructive_actions", payload.AllowDestructiveActions, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		h.notifySettingsChanged(c, "allow_destructive_actions", strconv.FormatBool(payload.AllowDestructiveActions))
	}

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// UpdateMFASettings godoc
// @Summary Update MFA enforcement defaults
// @Description Configures whether MFA is required for all users.
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body UpdateMFASettingsRequest true "MFA settings payload"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/mfa [put]
func (h *AdminHandler) UpdateMFASettings(c *gin.Context) {
	email := c.GetString("user_email")

	var payload UpdateMFASettingsRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if h.database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database unavailable"})
		return
	}

	settings, err := h.database.GetMFASettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mfa settings"})
		return
	}
	settings.RequireAllUsers = payload.RequireAllUsers

	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if err := h.database.UpsertMFASettings(settings, uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save mfa settings"})
		return
	}

	if h.mfaSettings != nil {
		*h.mfaSettings = *settings
	} else {
		h.mfaSettings = settings
	}

	h.notifySettingsChanged(c, "mfa.require_all_users", strconv.FormatBool(settings.RequireAllUsers))

	c.JSON(http.StatusOK, StatusResponse{Status: "updated"})
}

// RefreshRackTLSCert godoc
// @Summary Refresh rack TLS certificate
// @Description Fetches and stores the latest TLS certificate for the configured Convox rack.
// @Tags Settings
// @Produce json
// @Success 200 {object} db.RackTLSCert
// @Failure 501 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/rack_tls_cert/refresh [post]
func (h *AdminHandler) RefreshRackTLSCert(c *gin.Context) {
	if h.config == nil || !h.config.RackTLSPinningEnabled || h.rackCertMgr == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "rack certificate manager not configured"})
		return
	}

	var uid *int64
	email := strings.TrimSpace(c.GetString("user_email"))
	if email != "" && h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	cert, err := h.rackCertMgr.Refresh(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, cert)
}

// GetCircleCISettings godoc
// @Summary Get CircleCI integration settings
// @Description Returns CircleCI integration configuration including API token and approval job name.
// @Tags Settings
// @Produce json
// @Success 200 {object} db.CircleCISettings
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/settings/circleci [get]
func (h *AdminHandler) GetCircleCISettings(c *gin.Context) {
	if h.database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database unavailable"})
		return
	}

	enabled, err := h.database.CircleCIEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check circleci status"})
		return
	}
	if !enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "circleci integration not configured"})
		return
	}

	settings, err := h.database.GetCircleCISettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get circleci settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

// Token management

// CreateAPIToken godoc
// @Summary Create an API token
// @Description Generates a new API token for automation or CI/CD use.
// @Tags API Tokens
// @Accept json
// @Produce json
// @Param request body CreateAPITokenRequest true "Token payload"
// @Success 200 {object} CreateAPITokenResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens [post]
func (h *AdminHandler) CreateAPIToken(c *gin.Context) {
	start := time.Now()
	var req CreateAPITokenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "api_token.create", strings.TrimSpace(req.UserEmail), err.Error(), start, nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)

	currentUser := c.GetString("user_email")
	targetEmail := req.UserEmail
	if targetEmail == "" {
		targetEmail = currentUser
	}

	// Get user ID
	user, err := h.database.GetUser(targetEmail)
	if err != nil {
		h.respondAuditError(c, http.StatusNotFound, "api_token.create", targetEmail, "user not found", start, nil)
		return
	}

	// Create token
	tokenReq := &token.APITokenRequest{
		Name:        req.Name,
		UserID:      user.ID,
		Permissions: req.Permissions,
	}
	if creatorEmail := strings.TrimSpace(c.GetString("user_email")); creatorEmail != "" && h.rbac != nil {
		if creator, err := h.rbac.GetUserWithID(creatorEmail); err == nil && creator != nil {
			id := creator.ID
			tokenReq.CreatedByUserID = &id
		}
	}

	resp, err := h.tokenService.GenerateAPIToken(tokenReq)
	if err != nil {
		details := map[string]interface{}{"name": tokenReq.Name}
		switch {
		case errors.Is(err, token.ErrAPITokenNameExists):
			h.respondAuditError(c, http.StatusBadRequest, "api_token.create", targetEmail, "token name already exists", start, details)
			return
		case errors.Is(err, token.ErrAPITokenNameRequired):
			h.respondAuditError(c, http.StatusBadRequest, "api_token.create", targetEmail, "token name is required", start, details)
			return
		default:
			h.respondAuditError(c, http.StatusInternalServerError, "api_token.create", targetEmail, "failed to create token", start, details)
			return
		}
	}

	h.notifyAPITokenCreated(c, targetEmail, req.Name)

	apiToken := *resp.APIToken
	payload := CreateAPITokenResponse{
		Token:       resp.Token,
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Permissions: apiToken.Permissions,
		APIToken:    apiToken,
	}
	details := map[string]interface{}{
		"name":        resp.APIToken.Name,
		"permissions": resp.APIToken.Permissions,
		"user_email":  targetEmail,
	}
	resource := fmt.Sprintf("%d", resp.APIToken.ID)
	h.respondAuditSuccess(c, http.StatusOK, payload, "api_token.create", resource, start, details)
}

func (h *AdminHandler) notifyAPITokenCreated(c *gin.Context, ownerEmail, tokenName string) {
	if h == nil || h.emailSender == nil {
		return
	}
	ownerEmail = strings.TrimSpace(ownerEmail)
	if ownerEmail == "" {
		return
	}
	inviter := h.currentAuthUser(c)
	creatorEmail := ""
	if inviter != nil {
		creatorEmail = strings.TrimSpace(inviter.Email)
	}
	if creatorEmail == "" {
		creatorEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	rack := h.rackDisplay()
	creatorLabel := creatorEmail
	if creatorLabel == "" {
		creatorLabel = "an administrator"
	}
	subjectOwner := fmt.Sprintf("Rack Gateway (%s): New API token created", rack)
	textOwner, htmlOwner, err := emailtemplates.RenderTokenCreatedOwner(rack, tokenName, creatorLabel)
	if err != nil || (textOwner == "" && htmlOwner == "") {
		textOwner = fmt.Sprintf("A new API token '%s' was created for your account by %s.", tokenName, creatorLabel)
	}
	_ = h.emailSender.Send(ownerEmail, subjectOwner, textOwner, htmlOwner)

	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	filtered := make([]string, 0, len(admins))
	for _, addr := range admins {
		if strings.EqualFold(addr, ownerEmail) {
			continue
		}
		filtered = append(filtered, addr)
	}
	if len(filtered) == 0 {
		return
	}
	sort.Strings(filtered)
	recipients := prioritiseInviterFirst(filtered, creatorEmail)
	subjectAdmin := fmt.Sprintf("Rack Gateway (%s): API token created for %s", rack, ownerEmail)
	textAdmin, htmlAdmin, err := emailtemplates.RenderTokenCreatedAdmin(rack, tokenName, ownerEmail, creatorLabel)
	if err != nil || (textAdmin == "" && htmlAdmin == "") {
		textAdmin = fmt.Sprintf("An API token '%s' was created for %s by %s.", tokenName, ownerEmail, creatorLabel)
	}
	_ = h.emailSender.SendMany(recipients, subjectAdmin, textAdmin, htmlAdmin)
}

// ListAPITokens godoc
// @Summary List API tokens
// @Description Returns all API tokens configured in the system.
// @Tags API Tokens
// @Produce json
// @Success 200 {array} db.APIToken
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens [get]
func (h *AdminHandler) ListAPITokens(c *gin.Context) {
	// List all API tokens
	tokens, err := h.database.ListAllAPITokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

// GetAPIToken godoc
// @Summary Get API token
// @Description Returns metadata for a specific API token.
// @Tags API Tokens
// @Produce json
// @Param tokenID path string true "Token ID"
// @Success 200 {object} db.APIToken
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens/{tokenID} [get]
func (h *AdminHandler) GetAPIToken(c *gin.Context) {
	tokenID := strings.TrimSpace(c.Param("tokenID"))
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	token, err := h.database.GetAPITokenByPublicID(tokenID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, token)
}

// UpdateAPIToken godoc
// @Summary Update an API token
// @Description Updates token metadata such as name and permissions.
// @Tags API Tokens
// @Accept json
// @Produce json
// @Param tokenID path string true "Token ID"
// @Param request body UpdateAPITokenRequest true "Token update"
// @Success 200 {object} db.APIToken
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens/{tokenID} [put]
func (h *AdminHandler) UpdateAPIToken(c *gin.Context) {
	start := time.Now()
	tokenIDStr := strings.TrimSpace(c.Param("tokenID"))
	if tokenIDStr == "" {
		h.respondAuditError(c, http.StatusBadRequest, "api_token.update", tokenIDStr, "invalid token ID", start, nil)
		return
	}

	var req UpdateAPITokenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, "api_token.update", tokenIDStr, err.Error(), start, nil)
		return
	}

	existing, err := h.database.GetAPITokenByPublicID(tokenIDStr)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "api_token.update", tokenIDStr, "failed to load token", start, nil)
		return
	}
	if existing == nil {
		h.respondAuditError(c, http.StatusNotFound, "api_token.update", tokenIDStr, "token not found", start, nil)
		return
	}

	tokenID := existing.ID

	details := make(map[string]interface{})

	if name := strings.TrimSpace(req.Name); name != "" && name != existing.Name {
		if err := h.tokenService.UpdateTokenName(tokenID, name); err != nil {
			switch {
			case errors.Is(err, token.ErrAPITokenNameExists):
				h.respondAuditError(c, http.StatusBadRequest, "api_token.update", tokenIDStr, "token name already exists", start, map[string]interface{}{"name": name})
				return
			case errors.Is(err, token.ErrAPITokenNameRequired):
				h.respondAuditError(c, http.StatusBadRequest, "api_token.update", tokenIDStr, "token name is required", start, nil)
				return
			default:
				h.respondAuditError(c, http.StatusInternalServerError, "api_token.update", tokenIDStr, "failed to update token name", start, map[string]interface{}{"name": name})
				return
			}
		}
		details["name"] = name
	}

	if req.Permissions != nil {
		if err := h.tokenService.UpdateTokenPermissions(tokenID, req.Permissions); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, "api_token.update", tokenIDStr, "failed to update token permissions", start, nil)
			return
		}
		details["permissions"] = req.Permissions
	}

	updated, err := h.database.GetAPITokenByID(tokenID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "api_token.update", tokenIDStr, "failed to load token", start, nil)
		return
	}
	if updated == nil {
		h.respondAuditError(c, http.StatusInternalServerError, "api_token.update", tokenIDStr, "token disappeared", start, nil)
		return
	}

	if len(details) == 0 {
		details["unchanged"] = true
	}
	details["current_name"] = updated.Name
	details["current_permissions"] = updated.Permissions

	h.respondAuditSuccess(c, http.StatusOK, updated, "api_token.update", tokenIDStr, start, details)
}

// DeleteAPIToken godoc
// @Summary Delete an API token
// @Description Permanently removes an API token.
// @Tags API Tokens
// @Param tokenID path string true "Token ID"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens/{tokenID} [delete]
func (h *AdminHandler) DeleteAPIToken(c *gin.Context) {
	start := time.Now()
	tokenIDStr := strings.TrimSpace(c.Param("tokenID"))
	if tokenIDStr == "" {
		h.respondAuditError(c, http.StatusBadRequest, "api_token.delete", tokenIDStr, "invalid token ID", start, nil)
		return
	}

	existing, err := h.database.GetAPITokenByPublicID(tokenIDStr)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "api_token.delete", tokenIDStr, "failed to load token", start, nil)
		return
	}
	if existing == nil {
		h.respondAuditError(c, http.StatusNotFound, "api_token.delete", tokenIDStr, "token not found", start, nil)
		return
	}

	if err := h.database.DeleteAPIToken(existing.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, "api_token.delete", tokenIDStr, "failed to delete token", start, nil)
		return
	}

	details := map[string]interface{}{}
	if strings.TrimSpace(existing.Name) != "" {
		details["name"] = existing.Name
	}
	h.respondAuditSuccess(c, http.StatusNoContent, nil, "api_token.delete", tokenIDStr, start, details)
}

func collectAllPermissions(rolePerms map[string][]string) []string {
	known := make(map[string]struct{})
	for _, perms := range rolePerms {
		for _, perm := range perms {
			known[perm] = struct{}{}
		}
	}
	for _, perm := range routematch.AllPermissions() {
		known[perm] = struct{}{}
	}
	known["convox:*:*"] = struct{}{}

	perms := make([]string, 0, len(known))
	wildcard := false
	for perm := range known {
		if perm == "convox:*:*" {
			wildcard = true
			continue
		}
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	if wildcard {
		perms = append(perms, "convox:*:*")
	}
	return perms
}

func buildRoleOptions(rolePerms map[string][]string) []RoleDescriptor {
	meta := rbac.RoleMetadataMap()
	ordered := rbac.RoleOrder()
	roles := make([]RoleDescriptor, 0, len(ordered))
	for _, role := range ordered {
		perms, ok := rolePerms[role]
		if !ok {
			continue
		}
		info, ok := meta[role]
		if !ok {
			continue
		}
		sorted := append([]string(nil), perms...)
		sort.Strings(sorted)
		roles = append(roles, RoleDescriptor{
			Name:        role,
			Label:       info.Label,
			Description: info.Description,
			Permissions: sorted,
		})
	}
	return roles
}

func flattenUserRoles(manager rbac.RBACManager, email string, rolePerms map[string][]string) ([]string, []string) {
	if manager == nil || email == "" {
		return nil, nil
	}

	roles, err := manager.GetUserRoles(email)
	if err != nil {
		return nil, nil
	}
	sort.Strings(roles)

	permSet := make(map[string]struct{})
	for _, role := range roles {
		if perms, ok := rolePerms[role]; ok {
			for _, perm := range perms {
				permSet[perm] = struct{}{}
			}
		}
	}

	perms := make([]string, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}
	sort.Strings(perms)

	return roles, perms
}

// GetTokenPermissionMetadata godoc
// @Summary Get token permission metadata
// @Description Returns the permission catalog and metadata used to build API token forms.
// @Tags API Tokens
// @Produce json
// @Success 200 {object} TokenPermissionMetadata
// @Failure 401 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens/permissions [get]
func (h *AdminHandler) GetTokenPermissionMetadata(c *gin.Context) {
	email := strings.TrimSpace(c.GetString("user_email"))
	if email == "" {
		if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok {
			email = strings.TrimSpace(authUser.Email)
		}
	}
	if email == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	rolePerms := rbac.DefaultRolePermissions()
	allPermissions := collectAllPermissions(rolePerms)
	roles := buildRoleOptions(rolePerms)
	userRoles, userPerms := flattenUserRoles(h.rbac, email, rolePerms)

	payload := TokenPermissionMetadata{
		Permissions:        allPermissions,
		Roles:              roles,
		DefaultPermissions: token.DefaultCICDPermissions(),
		UserRoles:          userRoles,
		UserPermissions:    userPerms,
	}

	c.JSON(http.StatusOK, payload)
}

func parseAuditTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}

	var lastErr error
	for _, layout := range layouts {
		if layout == "2006-01-02T15:04" || layout == "2006-01-02T15:04:05" {
			if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return t.UTC(), nil
			} else {
				lastErr = err
			}
			continue
		}
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		} else {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unable to parse time %q", value)
	}
	return time.Time{}, lastErr
}

func (h *AdminHandler) auditFiltersFromRequest(c *gin.Context) (db.AuditLogFilters, int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		limit = 100
	}

	userFilter := c.Query("user")
	if userFilter == "" {
		if userIDParam := c.Query("user_id"); userIDParam != "" {
			if userID, convErr := strconv.ParseInt(userIDParam, 10, 64); convErr == nil {
				user, lookupErr := h.database.GetUserByID(userID)
				if lookupErr != nil {
					return db.AuditLogFilters{}, 0, 0, lookupErr
				}
				if user != nil {
					userFilter = user.Email
				}
			}
		}
	}

	statusFilter := c.Query("status")
	actionTypeFilter := c.Query("action_type")
	resourceTypeFilter := c.Query("resource_type")
	searchFilter := c.Query("search")
	rangeFilter := strings.TrimSpace(c.DefaultQuery("range", "24h"))
	startParam := c.Query("start")
	endParam := c.Query("end")
	missingUserForID := false

	var (
		since      time.Time
		until      time.Time
		hasStart   bool
		hasEnd     bool
		startError error
		endError   error
	)

	if strings.TrimSpace(startParam) != "" {
		parsed, parseErr := parseAuditTime(startParam)
		if parseErr != nil {
			startError = parseErr
		} else {
			since = parsed
			hasStart = true
		}
	}
	if strings.TrimSpace(endParam) != "" {
		parsed, parseErr := parseAuditTime(endParam)
		if parseErr != nil {
			endError = parseErr
		} else {
			until = parsed
			hasEnd = true
		}
	}

	if startError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidStartTime
	}
	if endError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidEndTime
	}
	if hasStart && hasEnd && until.Before(since) {
		return db.AuditLogFilters{}, 0, 0, errInvalidTimeRange
	}

	if !hasStart {
		now := time.Now()
		switch rangeFilter {
		case "15m":
			since = now.Add(-15 * time.Minute)
			hasStart = true
		case "1h":
			since = now.Add(-1 * time.Hour)
			hasStart = true
		case "24h":
			since = now.Add(-24 * time.Hour)
			hasStart = true
		case "7d":
			since = now.Add(-7 * 24 * time.Hour)
			hasStart = true
		case "30d":
			since = now.Add(-30 * 24 * time.Hour)
			hasStart = true
		case "all":
			// no lower bound
		case "custom":
			// rely on explicit start/end parameters
		default:
			// fallback to 24h
			since = now.Add(-24 * time.Hour)
			hasStart = true
		}
	} else {
		// Ensure "custom" is reflected for URL sync if explicit start is provided without range
		if rangeFilter == "" {
			rangeFilter = "custom"
		}
	}

	resolvedUserEmail := userFilter
	if userFilter == "" && c.Query("user_id") != "" {
		missingUserForID = true
	}

	filters := db.AuditLogFilters{
		UserEmail:    resolvedUserEmail,
		Status:       statusFilter,
		ActionType:   actionTypeFilter,
		ResourceType: resourceTypeFilter,
		Search:       searchFilter,
		Range:        rangeFilter,
		Limit:        limit,
		Offset:       (page - 1) * limit,
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}
	if hasStart {
		filters.Since = since
	}
	if hasEnd {
		filters.Until = until
	}
	if missingUserForID {
		filters.UserEmail = fmt.Sprintf("__missing_user_%s__", strings.TrimSpace(c.Query("user_id")))
	}

	return filters, page, limit, nil
}
