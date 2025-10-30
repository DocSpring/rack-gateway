package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// shouldEnforceMFA is a local wrapper for db.ShouldEnforceMFA to maintain backward compatibility
// within the handlers package.
func shouldEnforceMFA(settings *db.MFASettings, user *db.User) bool {
	return db.ShouldEnforceMFA(settings, user)
}

// isMFAChallengeRequired is a local wrapper for db.IsMFAChallengeRequired to maintain backward compatibility
// within the handlers package.
func isMFAChallengeRequired(settings *db.MFASettings, user *db.User) bool {
	return db.IsMFAChallengeRequired(settings, user)
}

// mfaUserContext holds the authenticated user and user record for MFA operations
type mfaUserContext struct {
	authUser   *auth.AuthUser
	userRecord *db.User
}

// loadMFAUserContext loads and validates the authenticated user for MFA operations.
// Returns the context and true if successful, or sends an error response and returns false.
func loadMFAUserContext(c *gin.Context, database *db.Database) (*mfaUserContext, bool) {
	authUser, ok := auth.GetAuthUser(c.Request.Context())
	if !ok || authUser == nil || authUser.IsAPIToken {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa requires user session"})
		return nil, false
	}

	userRecord, err := database.GetUser(authUser.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return nil, false
	}
	if userRecord == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authorized"})
		return nil, false
	}

	return &mfaUserContext{
		authUser:   authUser,
		userRecord: userRecord,
	}, true
}

// parseIDParam parses and validates an integer ID from a path parameter.
// Returns the ID and true if valid, or sends an error response and returns false.
func parseIDParam(c *gin.Context, paramName string) (int64, bool) {
	idParam := strings.TrimSpace(c.Param(paramName))
	if idParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%s required", paramName)})
		return 0, false
	}

	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid %s", paramName)})
		return 0, false
	}

	return id, true
}

// loadMFAMethod loads an MFA method and validates it belongs to the user.
// Returns the method and true if valid, or sends an error response and returns false.
func loadMFAMethod(c *gin.Context, database *db.Database, methodID int64, userID int64) (*db.MFAMethod, bool) {
	method, err := database.GetMFAMethodByID(methodID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load mfa method"})
		return nil, false
	}
	if method == nil || method.UserID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "mfa method not found"})
		return nil, false
	}
	return method, true
}

// loadTrustedDevice loads a trusted device and validates it belongs to the user.
// Returns the device and true if valid, or sends an error response and returns false.
func loadTrustedDevice(c *gin.Context, database *db.Database, deviceID int64, userID int64) (*db.TrustedDevice, bool) {
	device, err := database.GetTrustedDeviceByID(deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load trusted device"})
		return nil, false
	}
	if device == nil || device.UserID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trusted device not found"})
		return nil, false
	}
	return device, true
}
