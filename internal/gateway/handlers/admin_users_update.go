package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// UpdateUser godoc
// @Summary Update user
// @Description Updates a user's email, name, and/or roles in a single request.
// @Tags Users
// @Accept json
// @Produce json
// @Param email path string true "Current email"
// @Param request body UpdateUserRequest true "User update payload"
// @Success 200 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email} [put]
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	start := time.Now()
	originalEmail := strings.TrimSpace(c.Param("email"))
	currentUserEmail := strings.TrimSpace(c.GetString("user_email"))

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, err.Error())
		return
	}

	if req.Email == nil && req.Name == nil && req.Roles == nil {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, "no fields provided")
		return
	}

	userConfig, err := h.loadUserForUpdate(c, originalEmail, start)
	if err != nil {
		return
	}

	currentEmail, emailChanged, err := h.updateUserEmail(c, originalEmail, req.Email, start)
	if err != nil {
		return
	}

	updatedName := h.updateUserName(userConfig.Name, req.Name)

	updatedRoles, rolesProvided, err := h.updateUserRoles(
		c,
		userConfig.Roles,
		req.Roles,
		currentEmail,
		currentUserEmail,
		originalEmail,
		start,
	)
	if err != nil {
		return
	}

	if err := h.rbac.SaveUser(currentEmail, &rbac.UserConfig{Name: updatedName, Roles: updatedRoles}); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()),
			currentEmail,
			"failed to update user",
			start,
			map[string]interface{}{"email": currentEmail},
		)
		return
	}

	h.respondUpdateUserSuccess(
		c,
		currentEmail,
		updatedName,
		updatedRoles,
		emailChanged,
		rolesProvided,
		start,
	)
}

// UpdateUserName godoc
// @Summary Update user name
// @Description Updates only a user's display name.
// @Tags Users
// @Accept json
// @Produce json
// @Param email path string true "User email"
// @Param request body UpdateUserNameRequest true "Name payload"
// @Success 200 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/name [put]
func (h *AdminHandler) UpdateUserName(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()),
			email,
			"email is required",
			start,
			nil,
		)
		return
	}

	var req UpdateUserNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()),
			email,
			err.Error(),
			start,
			nil,
		)
		return
	}

	userConfig, err := h.rbac.GetUser(email)
	if err != nil || userConfig == nil {
		h.respondAuditError(
			c,
			http.StatusNotFound,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()),
			email,
			"user not found",
			start,
			nil,
		)
		return
	}

	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()),
			email,
			"name cannot be empty",
			start,
			nil,
		)
		return
	}

	if err := h.rbac.SaveUser(email, &rbac.UserConfig{Name: newName, Roles: userConfig.Roles}); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()),
			email,
			"failed to update user name",
			start,
			nil,
		)
		return
	}

	payload := UserSummary{
		Email: email,
		Name:  newName,
		Roles: userConfig.Roles,
	}
	details := map[string]interface{}{"name": newName}

	h.respondUserSuccess(
		c,
		http.StatusOK,
		payload,
		rbac.ActionUpdateName,
		email,
		start,
		details,
	)
}

// loadUserForUpdate loads user config and database user for update operations.
func (h *AdminHandler) loadUserForUpdate(
	c *gin.Context,
	email string,
	start time.Time,
) (*rbac.UserConfig, error) {
	updateAction := userAuditAction(rbac.ActionUpdate)
	userConfig, err := h.rbac.GetUser(email)
	if err != nil || userConfig == nil {
		h.respondUserNotFound(c, rbac.ActionUpdate, email, start)
		return nil, fmt.Errorf("user not found")
	}

	dbUser, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			updateAction,
			email,
			"failed to load user",
			start,
			nil,
		)
		return nil, err
	}
	if dbUser == nil {
		h.respondUserNotFound(c, rbac.ActionUpdate, email, start)
		return nil, fmt.Errorf("user not found")
	}

	return userConfig, nil
}

// updateUserEmail updates the user email if provided, returning the current email and whether it changed.
func (h *AdminHandler) updateUserEmail(
	c *gin.Context,
	originalEmail string,
	newEmail *string,
	start time.Time,
) (string, bool, error) {
	currentEmail := originalEmail
	updateAction := userAuditAction(rbac.ActionUpdate)

	if newEmail == nil {
		return currentEmail, false, nil
	}

	trimmed := strings.TrimSpace(*newEmail)
	if trimmed == "" {
		h.respondAuditError(c, http.StatusBadRequest, updateAction, originalEmail, "email cannot be empty", start, nil)
		return "", false, fmt.Errorf("email cannot be empty")
	}

	updatedEmail := trimmed
	emailChanged := !strings.EqualFold(updatedEmail, currentEmail)

	if !emailChanged {
		return currentEmail, false, nil
	}

	existing, err := h.database.GetUser(updatedEmail)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			updateAction,
			originalEmail,
			"failed to check email availability",
			start,
			nil,
		)
		return "", false, err
	}

	if existing != nil {
		h.respondAuditError(
			c,
			http.StatusConflict,
			updateAction,
			originalEmail,
			"email already in use",
			start,
			map[string]interface{}{"email": updatedEmail},
		)
		return "", false, fmt.Errorf("email already in use")
	}

	if err := h.database.UpdateUserEmail(originalEmail, updatedEmail); err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			updateAction,
			originalEmail,
			"failed to update email",
			start,
			map[string]interface{}{"email": updatedEmail},
		)
		return "", false, err
	}

	return updatedEmail, true, nil
}

// updateUserName updates the user name if provided.
func (h *AdminHandler) updateUserName(currentName string, newName *string) string {
	if newName == nil {
		return currentName
	}
	trimmed := strings.TrimSpace(*newName)
	if trimmed == "" {
		return currentName
	}
	return trimmed
}

// updateUserRoles updates the user roles if provided.
func (h *AdminHandler) updateUserRoles(
	c *gin.Context,
	currentRoles []string,
	newRoles []string,
	userEmail string,
	currentUserEmail string,
	originalEmail string,
	start time.Time,
) ([]string, bool, error) {
	if newRoles == nil {
		return currentRoles, false, nil
	}

	if len(newRoles) == 0 {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, "roles cannot be empty")
		return nil, false, fmt.Errorf("roles cannot be empty")
	}

	if userEmail == currentUserEmail {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, "cannot change your own roles")
		return nil, false, fmt.Errorf("cannot change your own roles")
	}

	if err := validateUserRoles(newRoles); err != nil {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, err.Error())
		return nil, false, err
	}

	return newRoles, true, nil
}

// respondUpdateUserSuccess sends a success response for user update.
func (h *AdminHandler) respondUpdateUserSuccess(
	c *gin.Context,
	email string,
	name string,
	roles []string,
	emailChanged bool,
	rolesProvided bool,
	start time.Time,
) {
	payload := UserSummary{
		Email: email,
		Name:  name,
		Roles: append([]string(nil), roles...),
	}

	if snapshot, err := h.database.GetUser(email); err == nil && snapshot != nil {
		payload.Email = snapshot.Email
		payload.Name = snapshot.Name
		payload.Roles = append([]string(nil), snapshot.Roles...)
	}

	details := map[string]interface{}{"name": name}
	if emailChanged {
		details["email"] = email
	}
	if rolesProvided {
		details["roles"] = roles
	}

	h.respondUserSuccess(
		c,
		http.StatusOK,
		payload,
		rbac.ActionUpdate,
		email,
		start,
		details,
	)
}
