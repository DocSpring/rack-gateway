package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestEnforceMFARequirementsMissingMappingFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(EnforceMFARequirements(nil, nil, nil))

	executed := false
	router.GET("/unmapped", func(c *gin.Context) {
		executed = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/unmapped", nil)

	router.ServeHTTP(rec, req)

	assert.False(t, executed, "handler should not execute when MFA mapping is missing")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "mfa_configuration_error")
}

func TestEnforceMFARequirementsUnknownLevelFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	perm := rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionCreate)
	original := rbac.MFARequirements[perm]
	rbac.MFARequirements[perm] = rbac.MFALevel(99)
	defer func() { rbac.MFARequirements[perm] = original }()

	router := gin.New()
	router.Use(EnforceMFARequirements(nil, nil, nil))

	executed := false
	router.POST("/api/v1/deploy-approval-requests", func(c *gin.Context) {
		executed = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploy-approval-requests", nil)

	router.ServeHTTP(rec, req)

	assert.False(t, executed, "handler should not execute when MFA level is unknown")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "mfa_configuration_error")
}
