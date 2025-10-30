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
	currentEmail := originalEmail
	currentUserEmail := strings.TrimSpace(c.GetString("user_email"))

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, err.Error(), start, nil)
		return
	}

	if req.Email == nil && req.Name == nil && req.Roles == nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "no fields provided", start, nil)
		return
	}

	userConfig, err := h.rbac.GetUser(originalEmail)
	if err != nil || userConfig == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "user not found", start, nil)
		return
	}

	dbUser, err := h.database.GetUser(originalEmail)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "failed to load user", start, nil)
		return
	}
	if dbUser == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "user not found", start, nil)
		return
	}

	updatedEmail := currentEmail
	if req.Email != nil {
		trimmed := strings.TrimSpace(*req.Email)
		if trimmed == "" {
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "email cannot be empty", start, nil)
			return
		}
		updatedEmail = trimmed
	}

	emailChanged := !strings.EqualFold(updatedEmail, currentEmail)
	if emailChanged {
		if existing, err := h.database.GetUser(updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "failed to check email availability", start, nil)
			return
		} else if existing != nil {
			h.respondAuditError(c, http.StatusConflict, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "email already in use", start, map[string]interface{}{"email": updatedEmail})
			return
		}
		if err := h.database.UpdateUserEmail(originalEmail, updatedEmail); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "failed to update email", start, map[string]interface{}{"email": updatedEmail})
			return
		}
		currentEmail = updatedEmail
	}

	updatedName := userConfig.Name
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed != "" {
			updatedName = trimmed
		}
	}

	updatedRoles := userConfig.Roles
	rolesProvided := req.Roles != nil
	if rolesProvided {
		if len(req.Roles) == 0 {
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "roles cannot be empty", start, nil)
			return
		}
		if currentEmail == currentUserEmail {
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, "cannot change your own roles", start, nil)
			return
		}
		validRoles := map[string]bool{"viewer": true, "ops": true, "deployer": true, "admin": true}
		for _, role := range req.Roles {
			if !validRoles[role] {
				h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), originalEmail, fmt.Sprintf("invalid role: %s", role), start, nil)
				return
			}
		}
		updatedRoles = req.Roles
	}

	if err := h.rbac.SaveUser(currentEmail, &rbac.UserConfig{Name: updatedName, Roles: updatedRoles}); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), currentEmail, "failed to update user", start, map[string]interface{}{"email": currentEmail})
		return
	}

	payload := UserSummary{
		Email: currentEmail,
		Name:  updatedName,
		Roles: append([]string(nil), updatedRoles...),
	}
	if snapshot, err := h.database.GetUser(currentEmail); err == nil && snapshot != nil {
		payload.Email = snapshot.Email
		payload.Name = snapshot.Name
		payload.Roles = append([]string(nil), snapshot.Roles...)
	}
	details := map[string]interface{}{"name": updatedName}
	if emailChanged {
		details["email"] = currentEmail
	}
	if rolesProvided {
		details["roles"] = updatedRoles
	}

	resourceIdentifier := currentEmail
	if dbUserWithID, err := h.rbac.GetUserWithID(currentEmail); err == nil && dbUserWithID != nil && dbUserWithID.ID > 0 {
		resourceIdentifier = fmt.Sprintf("%d", dbUserWithID.ID)
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdate.String()), resourceIdentifier, start, details)
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
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), email, "email is required", start, nil)
		return
	}

	var req UpdateUserNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), email, err.Error(), start, nil)
		return
	}

	userConfig, err := h.rbac.GetUser(email)
	if err != nil || userConfig == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), email, "user not found", start, nil)
		return
	}

	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), email, "name cannot be empty", start, nil)
		return
	}

	if err := h.rbac.SaveUser(email, &rbac.UserConfig{Name: newName, Roles: userConfig.Roles}); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), email, "failed to update user name", start, nil)
		return
	}

	payload := UserSummary{
		Email: email,
		Name:  newName,
		Roles: userConfig.Roles,
	}
	details := map[string]interface{}{"name": newName}

	resourceIdentifier := email
	if dbUserWithID, err := h.rbac.GetUserWithID(email); err == nil && dbUserWithID != nil && dbUserWithID.ID > 0 {
		resourceIdentifier = fmt.Sprintf("%d", dbUserWithID.ID)
	}

	h.respondAuditSuccess(c, http.StatusOK, payload, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionUpdateName.String()), resourceIdentifier, start, details)
}
