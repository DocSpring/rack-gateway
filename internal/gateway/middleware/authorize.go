package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Authorize enforces the access policy declared for each authenticated gateway route in
// rbac's route table. It must run after Authenticated. Routes without a declared policy are denied.
func Authorize(manager rbac.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		spec, ok := rbac.LookupHTTPRoute(c.Request.Method, c.FullPath())
		if !ok {
			gtwlog.Errorf("authz: no policy for method=%s path=%s", c.Request.Method, c.FullPath())
			abortForbidden(c, "no authorization policy for this endpoint")
			return
		}

		authUser, ok := auth.GetAuthUser(c.Request.Context())
		if !ok || authUser == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		if authUser.IsAPIToken && !spec.AllowAPIToken {
			abortForbidden(c, "API tokens cannot use this endpoint")
			return
		}

		if isOwnAccount(c, spec, authUser) {
			c.Next()
			return
		}

		allowed, missing, err := routeAllowed(manager, spec, authUser.Principal())
		if err != nil {
			gtwlog.Errorf("authz: failed to check permissions for %s %s: %v", spec.Method, spec.Pattern, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
			return
		}
		if !allowed {
			abortForbidden(c, insufficientPermissionsMessage(missing))
			return
		}

		c.Next()
	}
}

// routeAllowed reports whether the principal may use the route and, when denied, the first
// permission it lacks (empty when the route has no usable policy).
func routeAllowed(manager rbac.Manager, spec rbac.RouteSpec, principal rbac.Principal) (bool, string, error) {
	switch spec.Access {
	case rbac.AccessAuthenticated:
		return true, "", nil
	case rbac.AccessPermissions:
		permissions := spec.PermissionStrings()
		if len(permissions) == 0 {
			return false, "", nil
		}
		for _, permission := range permissions {
			allowed, err := manager.Authorize(principal, permission)
			if err != nil {
				return false, "", err
			}
			if !allowed {
				return false, permission, nil
			}
		}
		return true, "", nil
	default:
		return false, "", nil
	}
}

// isOwnAccount reports whether a human caller is using a self-service route for their own account.
// The path parameter must match the caller's email exactly (handlers look users up by exact email).
// API tokens, and suspended or locked users (whom RBAC also denies), never qualify.
func isOwnAccount(c *gin.Context, spec rbac.RouteSpec, authUser *auth.User) bool {
	if spec.SelfParam == "" || authUser.IsAPIToken {
		return false
	}
	user := authUser.DBUser
	if user == nil || user.Suspended || user.LockedAt != nil {
		return false
	}
	target := strings.TrimSpace(c.Param(spec.SelfParam))
	return target != "" && target == user.Email
}

// insufficientPermissionsMessage names the missing permission so CLI and UI users can tell
// what access they need.
func insufficientPermissionsMessage(missing string) string {
	if missing == "" {
		return "insufficient permissions"
	}
	return "insufficient permissions: requires " + missing
}

func abortForbidden(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": message})
}
