package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// API token ownership rules.
//
// The /api-tokens routes require gateway:api_token:<action>, which lets a user work with the tokens
// they own. Seeing or changing another user's tokens also requires gateway:api_token:manage. A
// token's permissions can never exceed what its owner's current roles grant (the same check RBAC
// applies to every request the token makes).

// permManageAnyAPIToken lets the caller see and change API tokens owned by other users.
var permManageAnyAPIToken = rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionManage)

func (h *AdminHandler) canManageAnyAPIToken(c *gin.Context) bool {
	return callerCan(c, h.rbac, permManageAnyAPIToken)
}

// callerUser returns the calling human's current user record, or nil.
func (h *AdminHandler) callerUser(c *gin.Context) *db.User {
	authUser := h.currentAuthUser(c)
	if authUser == nil || authUser.IsAPIToken {
		return nil
	}
	return authUser.DBUser
}

// callerMayAccessToken reports whether the caller may see or change the token. Callers who may
// not are told the token doesn't exist, so token IDs of other users can't be probed.
func (h *AdminHandler) callerMayAccessToken(c *gin.Context, apiToken *db.APIToken) bool {
	if apiToken == nil {
		return false
	}
	caller := h.callerUser(c)
	if caller != nil && caller.ID == apiToken.UserID {
		return true
	}
	return h.canManageAnyAPIToken(c)
}

// visibleAPITokens lists every token for token managers and the caller's own tokens otherwise.
func (h *AdminHandler) visibleAPITokens(c *gin.Context) ([]*db.APIToken, error) {
	if h.canManageAnyAPIToken(c) {
		return h.database.ListAllAPITokens()
	}
	caller := h.callerUser(c)
	if caller == nil {
		return []*db.APIToken{}, nil
	}
	tokens, err := h.database.ListAPITokensByUser(caller.ID)
	if tokens == nil && err == nil {
		tokens = []*db.APIToken{}
	}
	return tokens, err
}

// permissionBeyondOwner returns the first requested permission the owner's current roles don't
// grant, or "" when the owner holds all of them.
func (h *AdminHandler) permissionBeyondOwner(owner *db.User, permissions []string) (string, error) {
	principal := rbac.UserPrincipal(owner)
	for _, permission := range permissions {
		allowed, err := h.rbac.Authorize(principal, permission)
		if err != nil {
			return "", err
		}
		if !allowed {
			return permission, nil
		}
	}
	return "", nil
}

// resolveNewTokenOwner loads the owner of a token being created (the caller, unless user_email names
// someone else), checks the caller may issue tokens for that owner, and checks the requested
// permissions are within the owner's role. It responds and returns ok=false on any failure.
func (h *AdminHandler) resolveNewTokenOwner(
	c *gin.Context,
	req *CreateAPITokenRequest,
	start time.Time,
) (*db.User, string, bool) {
	action := audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionCreate.String())
	caller := h.callerUser(c)
	targetEmail := strings.TrimSpace(req.UserEmail)
	if targetEmail == "" && caller != nil {
		targetEmail = caller.Email
	}

	// Email addresses are case-insensitive; resolve the caller's own address to the stored spelling.
	isSelf := caller != nil && strings.EqualFold(targetEmail, caller.Email)
	if isSelf {
		targetEmail = caller.Email
	}
	if !isSelf && !h.canManageAnyAPIToken(c) {
		message := "insufficient permissions: requires " + permManageAnyAPIToken
		h.respondAuditError(c, http.StatusForbidden, action, targetEmail, message, start, nil)
		return nil, "", false
	}

	owner, err := h.database.GetUser(targetEmail)
	if err != nil || owner == nil {
		h.respondAuditError(c, http.StatusNotFound, action, targetEmail, "user not found", start, nil)
		return nil, "", false
	}

	if !h.respondIfBeyondOwnerRole(c, owner, req.Permissions, action, targetEmail, start) {
		return nil, "", false
	}
	return owner, targetEmail, true
}

// ensureTokenWithinOwnerRole checks updated permissions for an existing token against its owner.
func (h *AdminHandler) ensureTokenWithinOwnerRole(
	c *gin.Context,
	ownerID int64,
	permissions []string,
	tokenIDStr string,
	start time.Time,
) bool {
	action := audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionUpdate.String())
	owner, err := h.database.GetUserByID(ownerID)
	if err != nil || owner == nil {
		message := "failed to load token owner"
		h.respondAuditError(c, http.StatusInternalServerError, action, tokenIDStr, message, start, nil)
		return false
	}
	return h.respondIfBeyondOwnerRole(c, owner, permissions, action, tokenIDStr, start)
}

// respondIfBeyondOwnerRole responds 403 and returns false when permissions exceed the owner's role.
func (h *AdminHandler) respondIfBeyondOwnerRole(
	c *gin.Context,
	owner *db.User,
	permissions []string,
	action string,
	resource string,
	start time.Time,
) bool {
	beyond, err := h.permissionBeyondOwner(owner, permissions)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, action, resource, "failed to check token permissions",
			start, nil)
		return false
	}
	if beyond != "" {
		message := "token permissions cannot exceed the owner's role: " + beyond
		h.respondAuditError(c, http.StatusForbidden, action, resource, message, start,
			map[string]interface{}{"permission": beyond})
		return false
	}
	return true
}
