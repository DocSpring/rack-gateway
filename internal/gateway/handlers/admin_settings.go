package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

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
