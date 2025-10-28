package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

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
