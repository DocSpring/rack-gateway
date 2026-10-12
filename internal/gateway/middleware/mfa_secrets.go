package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// RevealsSecrets reports whether an env read asks for secret values (?secrets=true). Without it they come back
// masked.
func RevealsSecrets(c *gin.Context) bool {
	return strings.EqualFold(c.Query("secrets"), "true")
}

// RequireMFAToRevealSecrets gives an env read that asks for secret values the MFA level of convox:secret:read (a
// recent step-up). The route's own permission (convox:env:read) needs no MFA, since its values come back masked.
func RequireMFAToRevealSecrets(
	mfaService MFAVerifier,
	database *db.Database,
	settings db.MFASettingsSource,
) gin.HandlerFunc {
	level := rbac.MFALevelForPermission(rbac.Convox(rbac.ResourceSecret, rbac.ActionRead))
	return func(c *gin.Context) {
		if !RevealsSecrets(c) {
			c.Next()
			return
		}
		enforceMFALevel(c, level, mfaService, database, settings)
	}
}
