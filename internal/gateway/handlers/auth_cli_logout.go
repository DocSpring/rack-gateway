package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

// CLILogoutResponse confirms a CLI logout.
type CLILogoutResponse struct {
	Revoked bool `json:"revoked"`
}

// CLILogout godoc
// @Summary Log out a CLI session
// @Description Revokes the session whose token is sent as `Authorization: Bearer <token>`. `rack-gateway logout`
// @Description calls it before removing the rack from its config. Cookies are ignored, so a browser can't be made
// @Description to call it, and API tokens get 401: they're revoked on the API Tokens page.
// @Tags Auth
// @Produce json
// @Success 200 {object} CLILogoutResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /auth/cli/logout [post]
func (h *AuthHandler) CLILogout(c *gin.Context) {
	sessionToken := extractBearerToken(c)
	email, name := h.sessionUser(c, sessionToken)
	if !h.recordAuthAttempt(c, audit.ActionScopeLogout, "cli", email, name) {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: audit.RefusalMessage(audit.ActionScopeLogout)})
		return
	}
	if email == "" {
		h.recordAuthResult(c, audit.ActionScopeLogout, "cli", "", "", audit.StatusFailed)
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "not a valid session"})
		return
	}

	_, err := h.sessions.RevokeByToken(sessionToken, nil)
	h.recordAuthResult(c, audit.ActionScopeLogout, "cli", email, name, auditStatus(err))
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to revoke session"})
		return
	}
	c.JSON(http.StatusOK, CLILogoutResponse{Revoked: true})
}
