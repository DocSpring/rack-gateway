package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// JWTAuth creates JWT authentication middleware
func JWTAuth(jwtManager *auth.JWTManager, rbacManager rbac.RBACManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check Authorization header first
		authHeader := c.GetHeader("Authorization")
		var token string

		if authHeader != "" {
			// Bearer token from header
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		} else {
			// Try cookie for web sessions
			if cookie, err := c.Cookie("session_token"); err == nil && cookie != "" {
				token = cookie
			}
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			c.Abort()
			return
		}

		// Validate JWT
		claims, err := jwtManager.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		// Check if user exists in RBAC
		user, err := rbacManager.GetUser(claims.Email)
		if err != nil || user == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "user not authorized"})
			c.Abort()
			return
		}

		// Store user in both gin context and request context for downstream handlers
		c.Set("user_email", claims.Email)
		c.Set("user_name", user.Name)
		c.Set("user_roles", user.Roles)

		authUser := &auth.AuthUser{
			Email:      claims.Email,
			Name:       user.Name,
			Roles:      user.Roles,
			IsAPIToken: false,
		}
		reqWithUser := c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContextKey, authUser))
		c.Request = reqWithUser

		c.Next()
	}
}

// CLIOnly creates middleware that only allows CLI authentication (no cookies)
func CLIOnly(authService *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var nextCalled bool
		authService.CLIOnlyMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			// Replace gin request with the authenticated request (carries context + headers)
			c.Request = r
		})).ServeHTTP(c.Writer, c.Request)

		if !nextCalled {
			// Authentication failed and response already written
			c.Abort()
			return
		}

		c.Next()
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

		// Check if user has any of the required roles
		hasRole := false
		for _, required := range roles {
			for _, userRole := range userRoleList {
				if userRole == required {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}
