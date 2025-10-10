package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
)

// CreateAPIToken godoc
// @Summary Create an API token
// @Description Generates a new API token for automation or CI/CD use.
// @Tags API Tokens
// @Accept json
// @Produce json
// @Param request body CreateAPITokenRequest true "Token payload"
// @Success 200 {object} CreateAPITokenResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens [post]
func (h *AdminHandler) CreateAPIToken(c *gin.Context) {
	start := time.Now()
	var req CreateAPITokenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), strings.TrimSpace(req.UserEmail), err.Error(), start, nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)

	// Validate that permissions are provided
	if len(req.Permissions) == 0 {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), strings.TrimSpace(req.UserEmail), "permissions required - specify --role or --permission", start, map[string]interface{}{"name": req.Name})
		return
	}

	currentUser := c.GetString("user_email")
	targetEmail := req.UserEmail
	if targetEmail == "" {
		targetEmail = currentUser
	}

	// Get user ID
	user, err := h.database.GetUser(targetEmail)
	if err != nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), targetEmail, "user not found", start, nil)
		return
	}

	// Create token
	tokenReq := &token.APITokenRequest{
		Name:        req.Name,
		UserID:      user.ID,
		Permissions: req.Permissions,
	}
	if creatorEmail := strings.TrimSpace(c.GetString("user_email")); creatorEmail != "" && h.rbac != nil {
		if creator, err := h.rbac.GetUserWithID(creatorEmail); err == nil && creator != nil {
			id := creator.ID
			tokenReq.CreatedByUserID = &id
		}
	}

	resp, err := h.tokenService.GenerateAPIToken(tokenReq)
	if err != nil {
		details := map[string]interface{}{"name": tokenReq.Name}
		switch {
		case errors.Is(err, token.ErrAPITokenNameExists):
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), targetEmail, "token name already exists", start, details)
			return
		case errors.Is(err, token.ErrAPITokenNameRequired):
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), targetEmail, "token name is required", start, details)
			return
		default:
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), targetEmail, "failed to create token", start, details)
			return
		}
	}

	h.notifyAPITokenCreated(c, targetEmail, req.Name)

	apiToken := *resp.APIToken
	payload := CreateAPITokenResponse{
		Token:       resp.Token,
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Permissions: apiToken.Permissions,
		APIToken:    apiToken,
	}
	details := map[string]interface{}{
		"name":        resp.APIToken.Name,
		"permissions": resp.APIToken.Permissions,
		"user_email":  targetEmail,
	}
	resource := fmt.Sprintf("%d", resp.APIToken.ID)
	h.respondAuditSuccess(c, http.StatusOK, payload, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringCreate), resource, start, details)
}

// ListAPITokens godoc
// @Summary List API tokens
// @Description Returns all API tokens configured in the system.
// @Tags API Tokens
// @Produce json
// @Success 200 {array} db.APIToken
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens [get]
func (h *AdminHandler) ListAPITokens(c *gin.Context) {
	// List all API tokens
	tokens, err := h.database.ListAllAPITokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

// GetAPIToken godoc
// @Summary Get API token
// @Description Returns metadata for a specific API token.
// @Tags API Tokens
// @Produce json
// @Param tokenID path string true "Token ID"
// @Success 200 {object} db.APIToken
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens/{tokenID} [get]
func (h *AdminHandler) GetAPIToken(c *gin.Context) {
	tokenID := strings.TrimSpace(c.Param("tokenID"))
	if tokenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	token, err := h.database.GetAPITokenByPublicID(tokenID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, token)
}

// UpdateAPIToken godoc
// @Summary Update an API token
// @Description Updates token metadata such as name and permissions.
// @Tags API Tokens
// @Accept json
// @Produce json
// @Param tokenID path string true "Token ID"
// @Param request body UpdateAPITokenRequest true "Token update"
// @Success 200 {object} db.APIToken
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens/{tokenID} [put]
func (h *AdminHandler) UpdateAPIToken(c *gin.Context) {
	start := time.Now()
	tokenIDStr := strings.TrimSpace(c.Param("tokenID"))
	if tokenIDStr == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "invalid token ID", start, nil)
		return
	}

	var req UpdateAPITokenRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, err.Error(), start, nil)
		return
	}

	existing, err := h.database.GetAPITokenByPublicID(tokenIDStr)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "failed to load token", start, nil)
		return
	}
	if existing == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "token not found", start, nil)
		return
	}

	tokenID := existing.ID

	details := make(map[string]interface{})

	if name := strings.TrimSpace(req.Name); name != "" && name != existing.Name {
		if err := h.tokenService.UpdateTokenName(tokenID, name); err != nil {
			switch {
			case errors.Is(err, token.ErrAPITokenNameExists):
				h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "token name already exists", start, map[string]interface{}{"name": name})
				return
			case errors.Is(err, token.ErrAPITokenNameRequired):
				h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "token name is required", start, nil)
				return
			default:
				h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "failed to update token name", start, map[string]interface{}{"name": name})
				return
			}
		}
		details["name"] = name
	}

	if req.Permissions != nil {
		if err := h.tokenService.UpdateTokenPermissions(tokenID, req.Permissions); err != nil {
			h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "failed to update token permissions", start, nil)
			return
		}
		details["permissions"] = req.Permissions
	}

	updated, err := h.database.GetAPITokenByID(tokenID)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "failed to load token", start, nil)
		return
	}
	if updated == nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, "token disappeared", start, nil)
		return
	}

	if len(details) == 0 {
		details["unchanged"] = true
	}
	details["current_name"] = updated.Name
	details["current_permissions"] = updated.Permissions

	h.respondAuditSuccess(c, http.StatusOK, updated, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringUpdate), tokenIDStr, start, details)
}

// DeleteAPIToken godoc
// @Summary Delete an API token
// @Description Permanently removes an API token.
// @Tags API Tokens
// @Param tokenID path string true "Token ID"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/tokens/{tokenID} [delete]
func (h *AdminHandler) DeleteAPIToken(c *gin.Context) {
	start := time.Now()
	tokenIDStr := strings.TrimSpace(c.Param("tokenID"))
	if tokenIDStr == "" {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringDelete), tokenIDStr, "invalid token ID", start, nil)
		return
	}

	existing, err := h.database.GetAPITokenByPublicID(tokenIDStr)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringDelete), tokenIDStr, "failed to load token", start, nil)
		return
	}
	if existing == nil {
		h.respondAuditError(c, http.StatusNotFound, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringDelete), tokenIDStr, "token not found", start, nil)
		return
	}

	if err := h.database.DeleteAPIToken(existing.ID); err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringDelete), tokenIDStr, "failed to delete token", start, nil)
		return
	}

	details := map[string]interface{}{}
	if strings.TrimSpace(existing.Name) != "" {
		details["name"] = existing.Name
	}
	h.respondAuditSuccess(c, http.StatusNoContent, nil, audit.BuildAction(rbac.ResourceStringAPIToken, rbac.ActionStringDelete), tokenIDStr, start, details)
}

// GetTokenPermissionMetadata godoc
// @Summary Get token permission metadata
// @Description Returns the permission catalog and metadata used to build API token forms.
// @Tags API Tokens
// @Produce json
// @Success 200 {object} TokenPermissionMetadata
// @Failure 401 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/tokens/permissions [get]
func (h *AdminHandler) GetTokenPermissionMetadata(c *gin.Context) {
	email := strings.TrimSpace(c.GetString("user_email"))
	if email == "" {
		if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok {
			email = strings.TrimSpace(authUser.Email)
		}
	}
	if email == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	rolePerms := rbac.DefaultRolePermissions()
	allPermissions := collectAllPermissions(rolePerms)
	roles := buildRoleOptions(rolePerms)
	userRoles, userPerms := flattenUserRoles(h.rbac, email, rolePerms)

	payload := TokenPermissionMetadata{
		Permissions:        allPermissions,
		Roles:              roles,
		DefaultPermissions: token.DefaultCICDPermissions(),
		UserRoles:          userRoles,
		UserPermissions:    userPerms,
	}

	c.JSON(http.StatusOK, payload)
}

func (h *AdminHandler) notifyAPITokenCreated(c *gin.Context, ownerEmail, tokenName string) {
	if h == nil || h.emailSender == nil {
		return
	}
	ownerEmail = strings.TrimSpace(ownerEmail)
	if ownerEmail == "" {
		return
	}
	inviter := h.currentAuthUser(c)
	creatorEmail := ""
	if inviter != nil {
		creatorEmail = strings.TrimSpace(inviter.Email)
	}
	if creatorEmail == "" {
		creatorEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	rack := h.rackDisplay()
	creatorLabel := creatorEmail
	if creatorLabel == "" {
		creatorLabel = "an administrator"
	}
	subjectOwner := fmt.Sprintf("Rack Gateway (%s): New API token created", rack)
	textOwner, htmlOwner, err := emailtemplates.RenderTokenCreatedOwner(rack, tokenName, creatorLabel)
	if err != nil || (textOwner == "" && htmlOwner == "") {
		textOwner = fmt.Sprintf("A new API token '%s' was created for your account by %s.", tokenName, creatorLabel)
	}
	_ = h.emailSender.Send(ownerEmail, subjectOwner, textOwner, htmlOwner)

	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	filtered := make([]string, 0, len(admins))
	for _, addr := range admins {
		if strings.EqualFold(addr, ownerEmail) {
			continue
		}
		filtered = append(filtered, addr)
	}
	if len(filtered) == 0 {
		return
	}
	sort.Strings(filtered)
	recipients := prioritiseInviterFirst(filtered, creatorEmail)
	subjectAdmin := fmt.Sprintf("Rack Gateway (%s): API token created for %s", rack, ownerEmail)
	textAdmin, htmlAdmin, err := emailtemplates.RenderTokenCreatedAdmin(rack, tokenName, ownerEmail, creatorLabel)
	if err != nil || (textAdmin == "" && htmlAdmin == "") {
		textAdmin = fmt.Sprintf("An API token '%s' was created for %s by %s.", tokenName, ownerEmail, creatorLabel)
	}
	_ = h.emailSender.SendMany(recipients, subjectAdmin, textAdmin, htmlAdmin)
}
