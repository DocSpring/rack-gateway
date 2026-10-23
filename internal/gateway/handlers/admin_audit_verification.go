package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// AuditVerificationResponse is the outcome of the gateway's latest scheduled audit verification. Status is null
// before the first run, and when the gateway doesn't verify its audit log (AUDIT_HMAC_SECRET isn't set, which only
// DEV_MODE allows).
type AuditVerificationResponse struct {
	Status *db.AuditVerificationStatus `json:"status"`
	// AnchorsConfigured is whether the gateway anchors the audit chain in S3 (AUDIT_ANCHOR_BUCKET). Without
	// anchors, verification can't detect events removed from the end of the chain, or the whole chain.
	AnchorsConfigured bool `json:"anchors_configured" validate:"required"`
}

// GetAuditVerification godoc
// @Summary Get the latest audit log verification
// @Description Returns the outcome of the gateway's hourly check of the audit hash chain and its S3 anchors.
// @Tags Audit
// @Produce json
// @Success 200 {object} AuditVerificationResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /audit-logs/verification [get]
func (h *AdminHandler) GetAuditVerification(c *gin.Context) {
	status, err := h.database.AuditVerificationStatus(c.Request.Context())
	if err != nil {
		CaptureErrorWithStatus(c, err, http.StatusInternalServerError, "failed to load the audit verification")
		return
	}
	anchored := h.config != nil && h.config.AuditAnchorsConfigured
	c.JSON(http.StatusOK, AuditVerificationResponse{Status: status, AnchorsConfigured: anchored})
}
