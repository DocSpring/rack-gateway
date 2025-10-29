package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"
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
	if h == nil || h.jobsClient == nil {
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

	// Enqueue welcome email to new user
	if recipient != "" {
		_, err := h.jobsClient.Insert(c.Request.Context(), jobemail.WelcomeArgs{
			Email:        recipient,
			Name:         req.Name,
			Roles:        req.Roles,
			InviterEmail: inviterEmail,
			Rack:         rack,
			BaseURL:      base,
		}, &river.InsertOpts{
			Queue:       jobs.QueueNotifications,
			MaxAttempts: jobs.MaxAttemptsNotification,
		})
		if err != nil {
			log.Printf("failed to enqueue welcome email to %s: %v", recipient, err)
		}
	}

	// Enqueue admin notification
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

	_, err := h.jobsClient.Insert(c.Request.Context(), jobemail.UserAddedAdminArgs{
		AdminEmails:  recipients,
		NewUserEmail: req.Email,
		NewUserName:  req.Name,
		Roles:        req.Roles,
		CreatorEmail: inviterEmail,
		Rack:         rack,
	}, &river.InsertOpts{
		Queue:       jobs.QueueNotifications,
		MaxAttempts: jobs.MaxAttemptsNotification,
	})
	if err != nil {
		log.Printf("failed to enqueue user added admin emails: %v", err)
	}
}
