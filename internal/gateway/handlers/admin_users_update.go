package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// UpdateUser godoc
// @Summary Update user
// @Description Updates a user's email, name, and/or roles in a single request. Every field is checked before
// @Description anything is written, and the changes are written together. A new email signs the user out
// @Description everywhere and deletes their API tokens.
// @Tags Users
// @Accept json
// @Produce json
// @Param email path string true "Current email"
// @Param request body UpdateUserRequest true "User update payload"
// @Success 200 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse "Email already in use, or the user's roles changed since they were read"
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email} [put]
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	start := time.Now()
	originalEmail := strings.TrimSpace(c.Param("email"))

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, err.Error())
		return
	}
	if req.Email == nil && req.Name == nil && req.Roles == nil {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, "no fields provided")
		return
	}

	target, caller, ok := h.loadUserAndAdmin(c, originalEmail, start, rbac.ActionUpdate.String())
	if !ok {
		return
	}
	update, problem := h.userUpdateFor(target, caller, &req)
	if problem != "" {
		h.respondUserBadRequest(c, rbac.ActionUpdate, originalEmail, start, problem)
		return
	}

	result, err := h.database.UpdateUser(target.ID, update, &caller.ID)
	if err != nil {
		h.respondUserUpdateError(c, rbac.ActionUpdate, originalEmail, err, start)
		return
	}

	details := map[string]interface{}{"name": result.User.Name}
	if result.EmailChanged {
		details["email"] = result.User.Email
		details["previous_email"] = target.Email
		details["revoked_sessions"] = result.RevokedSessions
		details["deleted_api_tokens"] = result.DeletedTokens
	}
	if update.Roles != nil {
		details["roles"] = update.Roles
	}
	summary := userSummary(result.User)
	h.respondUserSuccess(c, http.StatusOK, summary, rbac.ActionUpdate, result.User.Email, start, details)
}

// userUpdateFor checks every field of req and returns the change to make, or why it can't be made. Nothing is
// written until every field passes.
func (h *AdminHandler) userUpdateFor(target, caller *db.User, req *UpdateUserRequest) (db.UserUpdate, string) {
	var update db.UserUpdate
	if req.Email != nil {
		email, err := normalizeUserEmail(*req.Email, h.rbac.GetAllowedDomain())
		if err != nil {
			return db.UserUpdate{}, err.Error()
		}
		update.Email = &email
	}
	if req.Name != nil {
		if name := strings.TrimSpace(*req.Name); name != "" {
			update.Name = &name
		}
	}
	if req.Roles != nil {
		if len(req.Roles) == 0 {
			return db.UserUpdate{}, "roles cannot be empty"
		}
		if err := validateUserRoles(req.Roles); err != nil {
			return db.UserUpdate{}, err.Error()
		}
		// Compared by user ID, so a new email in the same request can't get past it.
		if target.ID == caller.ID && !db.SameRoles(target.Roles, req.Roles) {
			return db.UserUpdate{}, "cannot change your own roles"
		}
		update.Roles = req.Roles
		// Checked again under the user's row lock: the roles may change before the update is written.
		update.RolesWere = target.Roles
	}
	return update, ""
}

// respondUserUpdateError answers a failed db.UpdateUser.
func (h *AdminHandler) respondUserUpdateError(
	c *gin.Context, action rbac.Action, email string, err error, start time.Time,
) {
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		h.respondUserNotFound(c, action, email, start)
	case errors.Is(err, db.ErrUserExists):
		h.respondAuditError(c, http.StatusConflict, userAuditAction(action), email, "email already in use", start, nil)
	case errors.Is(err, db.ErrUserRolesChanged):
		h.respondAuditError(c, http.StatusConflict, userAuditAction(action), email,
			"the user's roles changed since they were read; reload and try again", start, nil)
	default:
		h.respondAuditError(
			c, http.StatusInternalServerError, userAuditAction(action), email, "failed to update user", start, nil,
		)
	}
}

func userSummary(user *db.User) UserSummary {
	return UserSummary{Email: user.Email, Name: user.Name, Roles: append([]string(nil), user.Roles...)}
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

	var req UpdateUserNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondUserBadRequest(c, rbac.ActionUpdateName, email, start, err.Error())
		return
	}
	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		h.respondUserBadRequest(c, rbac.ActionUpdateName, email, start, "name cannot be empty")
		return
	}

	target, caller, ok := h.loadUserAndAdmin(c, email, start, rbac.ActionUpdateName.String())
	if !ok {
		return
	}
	// Only the name is written: a name change never writes back roles another admin changed meanwhile.
	result, err := h.database.UpdateUser(target.ID, db.UserUpdate{Name: &newName}, &caller.ID)
	if err != nil {
		h.respondUserUpdateError(c, rbac.ActionUpdateName, email, err, start)
		return
	}

	h.respondUserSuccess(c, http.StatusOK, userSummary(result.User), rbac.ActionUpdateName, email, start,
		map[string]interface{}{"name": newName})
}
