package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
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
		h.respondUserBadRequest(c, rbac.ActionCreate, req.Email, start, err.Error())
		return
	}

	if err := validateUserRoles(req.Roles); err != nil {
		h.respondUserBadRequest(c, rbac.ActionCreate, req.Email, start, err.Error())
		return
	}

	userConfig := &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}

	if err := h.rbac.SaveUser(req.Email, userConfig); err != nil {
		h.handleCreateUserError(c, req.Email, err, start)
		return
	}

	h.trackUserCreation(c, req.Email)

	payload := UserSummary{
		Email:          req.Email,
		Name:           req.Name,
		Roles:          req.Roles,
		CreatedByEmail: strings.TrimSpace(c.GetString("user_email")),
	}

	details := h.buildUserDetails(req.Email, req.Name, req.Roles)

	h.respondUserSuccess(
		c,
		http.StatusCreated,
		payload,
		rbac.ActionCreate,
		req.Email,
		start,
		details,
	)

	h.notifyUserCreated(c, req)
}

// validateUserRoles validates that all roles are valid.
func validateUserRoles(roles []string) error {
	validRoles := map[string]bool{
		"viewer":   true,
		"ops":      true,
		"deployer": true,
		"admin":    true,
	}

	for _, role := range roles {
		if !validRoles[role] {
			return fmt.Errorf("invalid role: %s", role)
		}
	}
	return nil
}

// handleCreateUserError handles errors during user creation.
func (h *AdminHandler) handleCreateUserError(
	c *gin.Context,
	email string,
	err error,
	start time.Time,
) {
	status := http.StatusInternalServerError
	message := "failed to create user"

	if strings.Contains(err.Error(), "already exists") {
		status = http.StatusConflict
		message = "user already exists"
	}

	h.respondAuditError(
		c,
		status,
		audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()),
		strings.TrimSpace(email),
		message,
		start,
		nil,
	)
}

// trackUserCreation tracks the user creation in the database.
func (h *AdminHandler) trackUserCreation(c *gin.Context, email string) {
	if h.database == nil || h.rbac == nil {
		return
	}

	creatorEmail := strings.TrimSpace(c.GetString("user_email"))
	if creatorEmail == "" {
		return
	}

	creator, err := h.rbac.GetUserWithID(creatorEmail)
	if err != nil || creator == nil {
		return
	}

	newUser, err := h.database.GetUser(email)
	if err != nil || newUser == nil {
		return
	}

	_, _ = h.database.CreateUserResource(creator.ID, "user", newUser.Email)
}

// getUserResourceID returns the resource ID for audit logging.
func (h *AdminHandler) getUserResourceID(email string) string {
	userWithID, err := h.rbac.GetUserWithID(email)
	if err != nil || userWithID == nil || userWithID.ID <= 0 {
		return strings.TrimSpace(email)
	}
	return fmt.Sprintf("%d", userWithID.ID)
}

// buildUserDetails builds the details map for audit logging.
func (_ *AdminHandler) buildUserDetails(email, name string, roles []string) map[string]interface{} {
	details := map[string]interface{}{
		"email": email,
		"roles": roles,
	}
	if strings.TrimSpace(name) != "" {
		details["name"] = name
	}
	return details
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
