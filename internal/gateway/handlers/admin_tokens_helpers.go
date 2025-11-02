package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

func (h *AdminHandler) notifyAPITokenCreated(c *gin.Context, ownerEmail, tokenName string) {
	if h == nil || h.emailSender == nil {
		return
	}
	ownerEmail = strings.TrimSpace(ownerEmail)
	if ownerEmail == "" {
		return
	}

	creatorEmail := h.getCreatorEmail(c)
	rack := h.rackDisplay()
	creatorLabel := getCreatorLabel(creatorEmail)

	h.sendTokenCreatedOwnerEmail(ownerEmail, tokenName, rack, creatorLabel)
	h.sendTokenCreatedAdminEmails(ownerEmail, tokenName, rack, creatorLabel, creatorEmail)
}

func (h *AdminHandler) convertRoleToPermissions(
	req *CreateAPITokenRequest,
	c *gin.Context,
	start time.Time,
) error {
	if req.Role == "" || len(req.Permissions) > 0 {
		return nil
	}

	rolePerms := rbac.DefaultRolePermissions()
	perms, found := rolePerms[req.Role]
	if !found {
		errorMsg := fmt.Sprintf(
			"invalid role: %s (valid roles: viewer, ops, deployer, cicd, admin)",
			req.Role,
		)
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionCreate.String()),
			strings.TrimSpace(req.UserEmail),
			errorMsg,
			start,
			map[string]interface{}{"name": req.Name, "role": req.Role},
		)
		return fmt.Errorf("invalid role")
	}

	req.Permissions = perms
	return nil
}

func (h *AdminHandler) handleTokenGenerationError(
	c *gin.Context,
	err error,
	tokenName string,
	targetEmail string,
	start time.Time,
) {
	details := map[string]interface{}{"name": tokenName}
	action := audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionCreate.String())

	switch {
	case errors.Is(err, token.ErrAPITokenNameExists):
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			action,
			targetEmail,
			"token name already exists",
			start,
			details,
		)
	case errors.Is(err, token.ErrAPITokenNameRequired):
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			action,
			targetEmail,
			"token name is required",
			start,
			details,
		)
	default:
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			action,
			targetEmail,
			"failed to create token",
			start,
			details,
		)
	}
}

func (h *AdminHandler) updateTokenNameIfChanged(
	c *gin.Context,
	tokenID int64,
	tokenIDStr string,
	existingName string,
	newName string,
	start time.Time,
	details map[string]interface{},
) error {
	name := strings.TrimSpace(newName)
	if name == "" || name == existingName {
		return nil
	}

	if err := h.tokenService.UpdateTokenName(tokenID, name); err != nil {
		action := audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionUpdate.String())

		switch {
		case errors.Is(err, token.ErrAPITokenNameExists):
			h.respondAuditError(
				c,
				http.StatusBadRequest,
				action,
				tokenIDStr,
				"token name already exists",
				start,
				map[string]interface{}{"name": name},
			)
			return err
		case errors.Is(err, token.ErrAPITokenNameRequired):
			h.respondAuditError(
				c,
				http.StatusBadRequest,
				action,
				tokenIDStr,
				"token name is required",
				start,
				nil,
			)
			return err
		default:
			h.respondAuditError(
				c,
				http.StatusInternalServerError,
				action,
				tokenIDStr,
				"failed to update token name",
				start,
				map[string]interface{}{"name": name},
			)
			return err
		}
	}

	details["name"] = name
	return nil
}

func (h *AdminHandler) getCreatorEmail(c *gin.Context) string {
	inviter := h.currentAuthUser(c)
	if inviter != nil {
		if email := strings.TrimSpace(inviter.Email); email != "" {
			return email
		}
	}
	return strings.TrimSpace(c.GetString("user_email"))
}

func getCreatorLabel(creatorEmail string) string {
	if creatorEmail != "" {
		return creatorEmail
	}
	return "an administrator"
}

func (h *AdminHandler) sendTokenCreatedOwnerEmail(
	ownerEmail string,
	tokenName string,
	rack string,
	creatorLabel string,
) {
	subject := fmt.Sprintf("Rack Gateway (%s): New API token created", rack)
	text, html, err := emailtemplates.RenderTokenCreatedOwner(rack, tokenName, creatorLabel)
	if err != nil || (text == "" && html == "") {
		text = fmt.Sprintf("A new API token '%s' was created for your account by %s.", tokenName, creatorLabel)
	}
	_ = h.emailSender.Send(ownerEmail, subject, text, html)
}

func (h *AdminHandler) sendTokenCreatedAdminEmails(
	ownerEmail string,
	tokenName string,
	rack string,
	creatorLabel string,
	creatorEmail string,
) {
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}

	filtered := h.filterAdminsExcludingOwner(admins, ownerEmail)
	if len(filtered) == 0 {
		return
	}

	sort.Strings(filtered)
	recipients := prioritiseInviterFirst(filtered, creatorEmail)

	subject := fmt.Sprintf("Rack Gateway (%s): API token created for %s", rack, ownerEmail)
	text, html, err := emailtemplates.RenderTokenCreatedAdmin(rack, tokenName, ownerEmail, creatorLabel)
	if err != nil || (text == "" && html == "") {
		text = fmt.Sprintf("An API token '%s' was created for %s by %s.", tokenName, ownerEmail, creatorLabel)
	}
	_ = h.emailSender.SendMany(recipients, subject, text, html)
}

func (h *AdminHandler) filterAdminsExcludingOwner(admins []string, ownerEmail string) []string {
	filtered := make([]string, 0, len(admins))
	for _, addr := range admins {
		if !strings.EqualFold(addr, ownerEmail) {
			filtered = append(filtered, addr)
		}
	}
	return filtered
}

func (h *AdminHandler) validateUpdateAPITokenRequest(
	c *gin.Context,
	start time.Time,
) (string, *UpdateAPITokenRequest, *db.APIToken, bool) {
	action := audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionUpdate.String())
	tokenIDStr := strings.TrimSpace(c.Param("tokenID"))
	if tokenIDStr == "" {
		h.respondAuditError(c, http.StatusBadRequest, action, tokenIDStr, "invalid token ID", start, nil)
		return "", nil, nil, false
	}

	var req UpdateAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, action, tokenIDStr, err.Error(), start, nil)
		return "", nil, nil, false
	}

	existing, err := h.database.GetAPITokenByPublicID(tokenIDStr)
	if err != nil {
		h.respondAuditError(c, http.StatusInternalServerError, action, tokenIDStr, "failed to load token", start, nil)
		return "", nil, nil, false
	}
	if existing == nil {
		h.respondAuditError(c, http.StatusNotFound, action, tokenIDStr, "token not found", start, nil)
		return "", nil, nil, false
	}

	return tokenIDStr, &req, existing, true
}
