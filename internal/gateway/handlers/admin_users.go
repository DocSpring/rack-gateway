package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// LockUserRequest represents the payload for locking a user account
type LockUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// ListUsers godoc
// @Summary List all gateway users
// @Description Returns every user configured in the gateway along with role assignments.
// @Tags Users
// @Produce json
// @Success 200 {array} db.User
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /users [get]
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
// @Router /users/{email} [get]
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
// @Router /users [post]
func (h *AdminHandler) CreateUser(c *gin.Context) {
	start := time.Now()
	var req CreateUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringCreate), strings.TrimSpace(req.Email), err.Error(), start, nil)
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
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringCreate), strings.TrimSpace(req.Email), fmt.Sprintf("invalid role: %s", role), start, nil)
			return
		}
	}

	userConfig := &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}

	if err := h.rbac.SaveUser(req.Email, userConfig); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			h.respondAuditError(c, http.StatusConflict, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringCreate), strings.TrimSpace(req.Email), "user already exists", start, nil)
			return
		}
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringCreate), strings.TrimSpace(req.Email), "failed to create user", start, nil)
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

	h.respondAuditSuccess(c, http.StatusCreated, payload, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringCreate), resource, start, details)

	h.notifyUserCreated(c, req)
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
// @Router /users/{email} [delete]
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	start := time.Now()
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't delete yourself
	if email == currentUser {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringDelete), strings.TrimSpace(email), "cannot delete yourself", start, nil)
		return
	}

	if err := h.rbac.DeleteUser(email); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringDelete), strings.TrimSpace(email), "failed to delete user", start, nil)
		return
	}

	h.respondAuditSuccess(c, http.StatusNoContent, nil, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringDelete), strings.TrimSpace(email), start, nil)
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
// @Router /users/{email} [put]
func (h *AdminHandler) UpdateUserProfile(c *gin.Context) {
	start := time.Now()
	originalEmail := strings.TrimSpace(c.Param("email"))
	currentEmail := originalEmail

	var req UpdateUserProfileRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, err.Error(), start, nil)
		return
	}

	userConfig, err := h.rbac.GetUser(originalEmail)
	if err != nil || userConfig == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "user not found", start, nil)
		return
	}

	dbUser, err := h.database.GetUser(originalEmail)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "failed to load user", start, nil)
		return
	}
	if dbUser == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "user not found", start, nil)
		return
	}

	updatedEmail := strings.TrimSpace(req.Email)
	if updatedEmail == "" {
		updatedEmail = currentEmail
	}

	emailChanged := !strings.EqualFold(updatedEmail, currentEmail)
	if emailChanged {
		if existing, err := h.database.GetUser(updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "failed to check email availability", start, nil)
			return
		} else if existing != nil {
			h.respondAuditError(c, http.StatusConflict, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "email already in use", start, map[string]interface{}{"email": updatedEmail})
			return
		}
		if err := h.database.UpdateUserEmail(originalEmail, updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), originalEmail, "failed to update email", start, map[string]interface{}{"email": updatedEmail})
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
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), currentEmail, "failed to update user", start, map[string]interface{}{"email": currentEmail})
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

	h.respondAuditSuccess(c, http.StatusOK, payload, audit.BuildAction(rbac.ResourceStringUser, rbac.ActionStringUpdate), currentEmail, start, details)
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
// @Router /users/{email}/roles [put]
func (h *AdminHandler) UpdateUserRoles(c *gin.Context) {
	start := time.Now()
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't change your own roles
	if email == currentUser {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUpdateRoles), strings.TrimSpace(email), "cannot change your own roles", start, nil)
		return
	}

	var req UpdateUserRolesRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUpdateRoles), strings.TrimSpace(email), err.Error(), start, nil)
		return
	}

	user, err := h.rbac.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUpdateRoles), strings.TrimSpace(email), "user not found", start, nil)
		return
	}

	user.Roles = req.Roles
	if err := h.rbac.SaveUser(email, user); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUpdateRoles), strings.TrimSpace(email), "failed to update roles", start, nil)
		return
	}

	payload := UserSummary{
		Email: email,
		Name:  user.Name,
		Roles: req.Roles,
	}
	details := map[string]interface{}{"roles": req.Roles}
	h.respondAuditSuccess(c, http.StatusOK, payload, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUpdateRoles), strings.TrimSpace(email), start, details)
}

// LockUser godoc
// @Summary Lock a user account
// @Description Locks a user account to prevent login
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Param request body LockUserRequest true "Lock reason"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/lock [post]
func (h *AdminHandler) LockUser(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "email is required", start, nil)
		return
	}

	var req LockUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "invalid request", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "user not found", start, nil)
		return
	}

	// Get current admin user
	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "failed to load admin user", start, nil)
		return
	}

	// Lock the account
	if err := h.database.LockUser(user.ID, req.Reason, &adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, "failed to lock user", start, nil)
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
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user locked successfully"}, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbLock), email, start, details)
}

// UnlockUser godoc
// @Summary Unlock a user account
// @Description Unlocks a previously locked user account
// @Tags admin
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/unlock [post]
func (h *AdminHandler) UnlockUser(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "email is required", start, nil)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "failed to load user", start, nil)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "user not found", start, nil)
		return
	}

	// Get current admin user
	authUser := h.currentAuthUser(c)
	if authUser == nil {
		h.respondAuditError(c, http.StatusUnauthorized, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "unauthorized", start, nil)
		return
	}
	adminUser, err := h.database.GetUser(authUser.Email)
	if err != nil || adminUser == nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "failed to load admin user", start, nil)
		return
	}

	// Unlock the account
	if err := h.database.UnlockUser(user.ID, adminUser.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, "failed to unlock user", start, nil)
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
	h.respondAuditSuccess(c, http.StatusOK, gin.H{"message": "user unlocked successfully"}, audit.BuildAction(rbac.ResourceStringUser, audit.ActionVerbUnlock), email, start, details)
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
