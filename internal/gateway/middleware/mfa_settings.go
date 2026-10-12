package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// mfaSettingsKey caches a request's MFA settings, so the middleware that checks MFA reads them once per request.
const mfaSettingsKey = "mfa_settings"

// currentMFASettings returns the MFA settings for this request, read from source the first time a middleware asks.
// They're read per request rather than at startup, so an admin's change applies at once, on every gateway instance.
// A request whose settings can't be read is refused: its MFA policy can't be checked. Without a source the settings
// are nil, which enforces MFA with the default windows.
func currentMFASettings(c *gin.Context, source db.MFASettingsSource) (*db.MFASettings, bool) {
	if source == nil {
		return nil, true
	}
	if cached, ok := c.Get(mfaSettingsKey); ok {
		if settings, ok := cached.(*db.MFASettings); ok {
			return settings, true
		}
	}
	settings, err := source.GetMFASettings()
	if err != nil {
		gtwlog.Errorf("mfa: failed to load MFA settings: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":   "mfa_settings_unavailable",
			"message": "MFA settings couldn't be loaded, so this request can't be checked. Please try again.",
		})
		return nil, false
	}
	c.Set(mfaSettingsKey, settings)
	return settings, true
}
