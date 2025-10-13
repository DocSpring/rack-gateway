package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

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
// @Router /settings/rack_tls_cert/refresh [post]
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
