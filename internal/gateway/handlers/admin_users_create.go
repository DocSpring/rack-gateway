package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
)

// CreateUser godoc
// @Summary Create a user
// @Description Creates a new gateway user with the provided roles.
// @Tags Users
// @Accept json
// @Produce json
// @Param request body CreateUserRequest true "User payload"
// @Success 201 {object} UserSummary
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users [post]
func (h *AdminHandler) CreateUser(c *gin.Context) {
	start := time.Now()
	var req CreateUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()), strings.TrimSpace(req.Email), err.Error(), start, nil)
		return
	}

	validRoles := []string{"viewer", "ops", "deployer", "admin"}
	for _, role := range req.Roles {
		matched := false
		for _, vr := range validRoles {
			if role == vr {
				matched = true
				break
			}
		}
		if !matched {
			h.respondAuditError(c, http.StatusBadRequest, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()), strings.TrimSpace(req.Email), fmt.Sprintf("invalid role: %s", role), start, nil)
			return
		}
	}

	userConfig := &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}

	if err := h.rbac.SaveUser(req.Email, userConfig); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			h.respondAuditError(c, http.StatusConflict, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()), strings.TrimSpace(req.Email), "user already exists", start, nil)
			return
		}
		h.respondAuditError(c, http.StatusInternalServerError, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()), strings.TrimSpace(req.Email), "failed to create user", start, nil)
		return
	}

	if h.database != nil && h.rbac != nil {
		if creatorEmail := strings.TrimSpace(c.GetString("user_email")); creatorEmail != "" {
			if creator, err := h.rbac.GetUserWithID(creatorEmail); err == nil && creator != nil {
				if newUser, err := h.database.GetUser(req.Email); err == nil && newUser != nil {
					_, _ = h.database.CreateUserResource(creator.ID, "user", newUser.Email)
				}
			}
		}
	}

	payload := UserSummary{
		Email:          req.Email,
		Name:           req.Name,
		Roles:          req.Roles,
		CreatedByEmail: strings.TrimSpace(c.GetString("user_email")),
	}

	resource := strings.TrimSpace(req.Email)
	if userWithID, err := h.rbac.GetUserWithID(req.Email); err == nil && userWithID != nil && userWithID.ID > 0 {
		resource = fmt.Sprintf("%d", userWithID.ID)
	}

	details := map[string]interface{}{"email": req.Email, "roles": req.Roles}
	if strings.TrimSpace(req.Name) != "" {
		details["name"] = req.Name
	}

	h.respondAuditSuccess(c, http.StatusCreated, payload, audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()), resource, start, details)

	h.notifyUserCreated(c, req)
}

func (h *AdminHandler) notifyUserCreated(c *gin.Context, req CreateUserRequest) {
	if h == nil || h.emailSender == nil {
		return
	}
	inviter := h.currentAuthUser(c)
	inviterEmail := ""
	if inviter != nil {
		inviterEmail = strings.TrimSpace(inviter.Email)
	}
	if inviterEmail == "" {
		inviterEmail = strings.TrimSpace(c.GetString("user_email"))
	}
	rack := h.rackDisplay()
	base := h.publicBaseURL(c)
	recipient := strings.TrimSpace(req.Email)
	if recipient != "" {
		subjectUser := fmt.Sprintf("Rack Gateway (%s): You've been granted access", rack)
		textUser, htmlUser, err := emailtemplates.RenderWelcome(rack, recipient, inviterEmail, base, base)
		if err != nil || (textUser == "" && htmlUser == "") {
			roles := strings.Join(req.Roles, ", ")
			textUser = fmt.Sprintf("You've been granted access to the Rack Gateway (%s) with roles: %s.", rack, roles)
		}
		_ = h.emailSender.Send(recipient, subjectUser, textUser, htmlUser)
	}

	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	filtered := make([]string, 0, len(admins))
	for _, addr := range admins {
		if strings.EqualFold(addr, req.Email) {
			continue
		}
		filtered = append(filtered, addr)
	}
	if len(filtered) == 0 {
		return
	}
	sort.Strings(filtered)
	recipients := prioritiseInviterFirst(filtered, inviterEmail)
	creator := inviterEmail
	if creator == "" {
		creator = "an administrator"
	}
	subjectAdmin := fmt.Sprintf("Rack Gateway (%s): %s added %s (%s)", rack, creator, req.Email, req.Name)
	textAdmin, htmlAdmin, err := emailtemplates.RenderUserAddedAdmin(rack, creator, req.Email, req.Name, req.Roles)
	if err != nil || (textAdmin == "" && htmlAdmin == "") {
		textAdmin = fmt.Sprintf("%s added new user %s (%s) with roles: %s.", creator, req.Email, req.Name, strings.Join(req.Roles, ", "))
	}
	_ = h.emailSender.SendMany(recipients, subjectAdmin, textAdmin, htmlAdmin)
}
