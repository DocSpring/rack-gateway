package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// ListUsers godoc
// @Summary List all gateway users
// @Description Returns every user configured in the gateway along with role assignments.
// @Description Callers without gateway:user:read get the team directory: lock reasons, who locked
// @Description the account, and MFA preferences are omitted.
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

	if !callerCan(c, h.rbac, rbac.Gateway(rbac.ResourceUser, rbac.ActionRead)) {
		for i := range users {
			users[i] = directoryEntry(users[i])
		}
	}

	c.JSON(http.StatusOK, users)
}

// directoryEntry returns the fields every teammate may see: identity, roles, account status and
// who created the account. Lock reasons, who locked the account, and MFA preferences stay with
// user administrators.
func directoryEntry(user *db.User) *db.User {
	if user == nil {
		return nil
	}
	return &db.User{
		ID:              user.ID,
		Email:           user.Email,
		Name:            user.Name,
		Roles:           user.Roles,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
		Suspended:       user.Suspended,
		MFAEnrolled:     user.MFAEnrolled,
		LockedAt:        user.LockedAt,
		CreatedByUserID: user.CreatedByUserID,
		CreatedByEmail:  user.CreatedByEmail,
		CreatedByName:   user.CreatedByName,
	}
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
