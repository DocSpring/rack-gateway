package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Authenticated enforces authentication for browser/admin API requests, supporting both session tokens and cookies.
func Authenticated(authService *auth.Service, _ rbac.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authService == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "authentication unavailable"})
			c.Abort()
			return
		}

		authUser, source, err := authService.AuthenticateHTTPRequest(c.Request)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		if authUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			c.Abort()
			return
		}

		if err := setUserRolesAndName(c, authUser); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		c.Set("user_email", authUser.Email)

		configureSentry(c, authUser, source)
		updateRequestContext(c, authUser, source)

		c.Next()
	}
}

// setUserRolesAndName sets user roles and name in the Gin context
func setUserRolesAndName(c *gin.Context, authUser *auth.User) error {
	if !authUser.IsAPIToken {
		dbUser := authUser.DBUser
		if dbUser == nil {
			return fmt.Errorf("user not authorized")
		}
		authUser.Roles = append([]string(nil), dbUser.Roles...)
		c.Set("user_roles", dbUser.Roles)
		c.Set("user_name", dbUser.Name)
	} else {
		c.Set("user_roles", []string{})
		c.Set("user_name", authUser.Name)
	}
	return nil
}

// configureSentry sets up Sentry user context and tags
func configureSentry(c *gin.Context, authUser *auth.User, source string) {
	hub := sentrygin.GetHubFromContext(c)
	if hub == nil {
		return
	}

	user := sentry.User{
		Email:    authUser.Email,
		Username: authUser.Name,
	}
	if authUser.IsAPIToken && authUser.TokenID != nil {
		user.ID = fmt.Sprintf("token:%d", *authUser.TokenID)
	}
	hub.Scope().SetUser(user)
	hub.Scope().SetTag("auth_source", source)
	hub.Scope().SetTag("auth_is_api_token", strconv.FormatBool(authUser.IsAPIToken))
	if len(authUser.Roles) > 0 {
		hub.Scope().SetTag("auth_roles", strings.Join(authUser.Roles, ","))
	}
}

// updateRequestContext updates the request context and headers with auth information
func updateRequestContext(c *gin.Context, authUser *auth.User, source string) {
	reqWithUser := c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
	c.Request = reqWithUser
	c.Request.Header.Set("X-User-Email", authUser.Email)
	c.Request.Header.Set("X-User-Name", authUser.Name)
	if source != "" {
		c.Request.Header.Set("X-Auth-Source", source)
	}
	setAPITokenHeaders(c.Request, authUser)
}

// setAPITokenHeaders sets or removes API token headers based on auth user type
func setAPITokenHeaders(r *http.Request, authUser *auth.User) {
	if !authUser.IsAPIToken {
		r.Header.Del("X-API-Token-ID")
		r.Header.Del("X-API-Token-Name")
		return
	}

	if authUser.TokenID != nil {
		r.Header.Set("X-API-Token-ID", fmt.Sprintf("%d", *authUser.TokenID))
	} else {
		r.Header.Del("X-API-Token-ID")
	}

	tokenName := strings.TrimSpace(authUser.TokenName)
	if tokenName != "" {
		r.Header.Set("X-API-Token-Name", tokenName)
	} else {
		r.Header.Del("X-API-Token-Name")
	}
}

// CLIOnly creates middleware that only allows CLI authentication (no cookies)
func CLIOnly(authService *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var nextCalled bool

		// CRITICAL: For WebSocket upgrades, we MUST bypass Gin's response writer wrapper
		// and use the raw http.ResponseWriter. Gin's wrapper buffers writes which breaks
		// WebSocket hijacking. This issue only manifests when there are slow operations
		// (like database queries) before the upgrade, giving Gin time to buffer headers.
		writer := http.ResponseWriter(c.Writer)
		isWebSocket := strings.Contains(strings.ToLower(c.Request.Header.Get("Connection")), "upgrade") &&
			strings.ToLower(c.Request.Header.Get("Upgrade")) == "websocket"

		authService.CLIOnlyMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			nextCalled = true
			// Replace gin request with the authenticated request (carries context + headers)
			c.Request = r
		})).ServeHTTP(writer, c.Request)

		if !nextCalled {
			// Authentication failed and response already written
			c.Abort()
			return
		}

		c.Next()

		// CRITICAL: After WebSocket upgrade, the connection is hijacked and Gin's writer
		// status is not updated. If the upgrade succeeded (Written=true), update the status
		// to 101 so downstream middleware (e.g., HTTP request logger) logs the correct status.
		if isWebSocket && c.Writer.Written() && c.Writer.Status() == http.StatusOK {
			c.Status(http.StatusSwitchingProtocols)
		}
	}
}

// RequireRole creates middleware that requires specific roles
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRoles, exists := c.Get("user_roles")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "no roles found"})
			c.Abort()
			return
		}

		userRoleList := userRoles.([]string)

		if !hasRequiredRole(userRoleList, roles) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// hasRequiredRole checks if the user has any of the required roles
func hasRequiredRole(userRoles, requiredRoles []string) bool {
	for _, required := range requiredRoles {
		for _, userRole := range userRoles {
			if userRole == required {
				return true
			}
		}
	}
	return false
}
