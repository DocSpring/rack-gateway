package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CLILoginInitiatorResponse says where a CLI login was started.
type CLILoginInitiatorResponse struct {
	Device string `json:"device,omitempty"`
	IP     string `json:"ip,omitempty"`
}

// CLILoginInitiator godoc
// @Summary Show where a CLI login was started
// @Description Returns the device name and IP address that started the CLI login bound to this browser, so the
// @Description MFA challenge page can show the user a login they didn't start. The page reads them from the
// @Description gateway, not from its own URL, which a crafted link could fill with a reassuring device or address.
// @Tags Auth
// @Produce json
// @Param state query string true "Login state"
// @Success 200 {object} CLILoginInitiatorResponse
// @Failure 400 {object} ErrorResponse
// @Router /auth/cli/initiator [get]
func (h *AuthHandler) CLILoginInitiator(c *gin.Context) {
	record, errCode := h.loadBoundCLILogin(c, strings.TrimSpace(c.Query("state")))
	if errCode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCode})
		return
	}
	c.JSON(http.StatusOK, CLILoginInitiatorResponse{
		Device: record.InitiatorDevice.String,
		IP:     record.InitiatorIP.String,
	})
}
