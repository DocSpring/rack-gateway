package middleware

import (
	"bytes"
	"io"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

// ExtractSettingsPermissions extracts RBAC permissions from settings operations.
// Returns nil if this is not a settings operation or if permissions cannot be determined.
func ExtractSettingsPermissions(c *gin.Context) []string {
	path := c.Request.URL.Path
	method := c.Request.Method

	// Global settings: /.gateway/api/admin/settings or /admin/settings (tests)
	if path == "/.gateway/api/admin/settings" || path == "/admin/settings" {
		switch method {
		case "PUT":
			return extractGlobalSettingsFromPUT(c)
		case "DELETE":
			return extractGlobalSettingsFromDELETE(c)
		}
	}

	// App settings: /.gateway/api/apps/:app/settings or /apps/:app/settings (tests)
	appName := c.Param("app")
	if appName != "" {
		expectedPath1 := "/.gateway/api/apps/" + appName + "/settings"
		expectedPath2 := "/apps/" + appName + "/settings"
		if path == expectedPath1 || path == expectedPath2 {
			switch method {
			case "PUT":
				return extractAppSettingsFromPUT(c)
			case "DELETE":
				return extractAppSettingsFromDELETE(c)
			}
		}
	}

	return nil
}

func extractGlobalSettingsFromPUT(c *gin.Context) []string {
	// Read body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil
	}
	// Restore body for handler
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	var updates map[string]interface{}
	// Parse without binding to avoid consuming body
	if err := c.ShouldBindJSON(&updates); err != nil {
		// Restore body again after failed parse
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return nil
	}
	// Restore body after successful parse
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	permissions := make([]string, 0, len(updates))
	for key := range updates {
		settingKey, err := settings.ParseGlobalSettingKey(key)
		if err != nil {
			continue
		}
		permissions = append(permissions, rbac.GatewayGlobalSetting(settingKey))
	}

	return permissions
}

func extractGlobalSettingsFromDELETE(c *gin.Context) []string {
	keys := c.QueryArray("key")
	permissions := make([]string, 0, len(keys))
	for _, key := range keys {
		settingKey, err := settings.ParseGlobalSettingKey(key)
		if err != nil {
			continue
		}
		permissions = append(permissions, rbac.GatewayGlobalSetting(settingKey))
	}
	return permissions
}

func extractAppSettingsFromPUT(c *gin.Context) []string {
	// Read body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil
	}
	// Restore body for handler
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return nil
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	permissions := make([]string, 0, len(updates))
	for key := range updates {
		settingKey, err := settings.ParseAppSettingKey(key)
		if err != nil {
			continue
		}
		permissions = append(permissions, rbac.GatewayAppSetting(settingKey))
	}

	return permissions
}

func extractAppSettingsFromDELETE(c *gin.Context) []string {
	keys := c.QueryArray("key")
	permissions := make([]string, 0, len(keys))
	for _, key := range keys {
		settingKey, err := settings.ParseAppSettingKey(key)
		if err != nil {
			continue
		}
		permissions = append(permissions, rbac.GatewayAppSetting(settingKey))
	}
	return permissions
}
