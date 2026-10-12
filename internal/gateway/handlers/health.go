package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
)

// HealthHandler handles health check endpoints
type HealthHandler struct{}

// NewHealthHandler creates a new health handler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health godoc
// @Summary Health check
// @Description Returns service health information. The gateway is unhealthy for a minute after an audit write
// @Description fails: it refuses every request it can't record.
// @Tags Health
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} HealthResponse
// @Router /health [get]
func (_ *HealthHandler) Health(c *gin.Context) {
	if err := audit.WriteHealth(time.Now()); err != nil {
		c.JSON(http.StatusServiceUnavailable, HealthResponse{
			Status:  "unhealthy",
			Service: "rack-gateway",
			Error:   err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, HealthResponse{
		Status:  "ok",
		Service: "rack-gateway",
	})
}
