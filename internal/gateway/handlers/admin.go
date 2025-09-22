package handlers

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/email"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/routematch"
	"github.com/DocSpring/convox-gateway/internal/gateway/token"
	"github.com/gin-gonic/gin"
)

// AdminHandler handles admin API endpoints
type AdminHandler struct {
	rbac         rbac.RBACManager
	database     *db.Database
	tokenService *token.Service
	emailSender  email.Sender
	config       *config.Config
}

type roleOption struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

var (
	errInvalidStartTime = errors.New("invalid start time")
	errInvalidEndTime   = errors.New("invalid end time")
	errInvalidTimeRange = errors.New("end time must be after start time")
)

// NewAdminHandler creates a new admin handler
func NewAdminHandler(rbac rbac.RBACManager, database *db.Database, tokenService *token.Service, emailSender email.Sender, config *config.Config) *AdminHandler {
	return &AdminHandler{
		rbac:         rbac,
		database:     database,
		tokenService: tokenService,
		emailSender:  emailSender,
		config:       config,
	}
}

// ListUsers returns all users
func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.database.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	c.JSON(http.StatusOK, users)
}

// CreateUser creates a new user
func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req struct {
		Email string   `json:"email" binding:"required,email"`
		Name  string   `json:"name" binding:"required"`
		Roles []string `json:"roles" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate roles
	validRoles := []string{"viewer", "ops", "deployer", "admin"}
	for _, role := range req.Roles {
		valid := false
		for _, vr := range validRoles {
			if role == vr {
				valid = true
				break
			}
		}
		if !valid {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid role: %s", role)})
			return
		}
	}

	// Create user
	userConfig := &rbac.UserConfig{
		Name:  req.Name,
		Roles: req.Roles,
	}

	if err := h.rbac.SaveUser(req.Email, userConfig); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
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

	// Send welcome email
	if h.emailSender != nil {
		// Would send email here
	}

	c.JSON(http.StatusCreated, gin.H{
		"email":            req.Email,
		"name":             req.Name,
		"roles":            req.Roles,
		"created_by_email": strings.TrimSpace(c.GetString("user_email")),
	})
}

// DeleteUser deletes a user
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't delete yourself
	if email == currentUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete yourself"})
		return
	}

	if err := h.rbac.DeleteUser(email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}

	c.Status(http.StatusNoContent)
}

// UpdateUserProfile updates user profile
func (h *AdminHandler) UpdateUserProfile(c *gin.Context) {
	email := c.Param("email")

	var req struct {
		Name string `json:"name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.rbac.GetUser(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	user.Name = req.Name
	if err := h.rbac.SaveUser(email, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"email": email,
		"name":  req.Name,
		"roles": user.Roles,
	})
}

// UpdateUserRoles updates user roles
func (h *AdminHandler) UpdateUserRoles(c *gin.Context) {
	email := c.Param("email")
	currentUser := c.GetString("user_email")

	// Can't change your own roles
	if email == currentUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change your own roles"})
		return
	}

	var req struct {
		Roles []string `json:"roles" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.rbac.GetUser(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	user.Roles = req.Roles
	if err := h.rbac.SaveUser(email, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update roles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"email": email,
		"name":  user.Name,
		"roles": req.Roles,
	})
}

// ListRoles returns all available roles
func (h *AdminHandler) ListRoles(c *gin.Context) {
	rolePerms := rbac.DefaultRolePermissions()
	metaMap := rbac.RoleMetadataMap()

	roles := make(map[string]interface{}, len(metaMap))
	for _, role := range rbac.RoleOrder() {
		meta, ok := metaMap[role]
		if !ok {
			continue
		}
		perms := rolePerms[role]
		if role == "admin" {
			perms = []string{"convox:*:*"}
		}
		roles[role] = map[string]interface{}{
			"name":        role,
			"label":       meta.Label,
			"description": meta.Description,
			"permissions": perms,
		}
	}

	c.JSON(http.StatusOK, roles)
}

// ListAuditLogs returns audit logs
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	filters, page, limit, err := h.auditFiltersFromRequest(c)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidStartTime):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time"})
		case errors.Is(err, errInvalidEndTime):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time"})
		case errors.Is(err, errInvalidTimeRange):
			c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		}
		return
	}

	logs, total, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  logs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// ExportAuditLogs exports audit logs as CSV
func (h *AdminHandler) ExportAuditLogs(c *gin.Context) {
	filters, _, _, err := h.auditFiltersFromRequest(c)
	if err != nil {
		switch {
		case errors.Is(err, errInvalidStartTime):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time"})
		case errors.Is(err, errInvalidEndTime):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time"})
		case errors.Is(err, errInvalidTimeRange):
			c.JSON(http.StatusBadRequest, gin.H{"error": "end time must be after start time"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch logs"})
		}
		return
	}

	if filters.Limit <= 0 || filters.Limit > 10000 {
		filters.Limit = 10000
	}
	filters.Offset = 0

	logs, _, err := h.database.GetAuditLogsPaged(filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch logs"})
		return
	}

	// Set CSV headers
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-logs-%s.csv\"", time.Now().Format("2006-01-02")))

	// Write CSV
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Header row
	writer.Write([]string{
		"timestamp", "user_email", "user_name", "action_type", "action",
		"command", "resource", "status", "response_time_ms", "ip_address", "user_agent",
	})

	// Data rows
	for _, log := range logs {
		writer.Write([]string{
			log.Timestamp.Format(time.RFC3339),
			log.UserEmail,
			log.UserName,
			log.ActionType,
			log.Action,
			log.Command,
			log.Resource,
			log.Status,
			strconv.Itoa(log.ResponseTimeMs),
			log.IPAddress,
			log.UserAgent,
		})
	}
}

// Config and settings handlers
func (h *AdminHandler) GetConfig(c *gin.Context) {
	// Get users from the manager
	users, err := h.rbac.GetUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get users"})
		return
	}

	// Convert internal format to API format
	apiConfig := gin.H{
		"domain": h.rbac.GetAllowedDomain(),
		"users":  users,
	}

	c.JSON(http.StatusOK, apiConfig)
}

func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	// Would update configuration
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

func (h *AdminHandler) GetSettings(c *gin.Context) {
	resp := make(map[string]interface{})

	if h.database != nil {
		if arr, err := h.database.GetProtectedEnvVars(); err == nil {
			resp["protected_env_vars"] = arr
		} else {
			resp["protected_env_vars"] = []string{}
		}
		if v, err := h.database.GetAllowDestructiveActions(); err == nil {
			resp["allow_destructive_actions"] = v
		} else {
			resp["allow_destructive_actions"] = false
		}
	} else {
		resp["protected_env_vars"] = []string{}
		resp["allow_destructive_actions"] = false
	}

	c.JSON(http.StatusOK, resp)
}

func (h *AdminHandler) UpdateProtectedEnvVars(c *gin.Context) {
	email := c.GetString("user_email")

	var payload struct {
		Protected []string `json:"protected_env_vars"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Normalize and de-dup to uppercase
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.Protected))
	for _, k := range payload.Protected {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}

	// Determine updating user id if available
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("protected_env_vars", out, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		// Send email notification to admins
		if h.emailSender != nil {
			// Would send email here
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *AdminHandler) UpdateAllowDestructiveActions(c *gin.Context) {
	email := c.GetString("user_email")

	var payload struct {
		Allow bool `json:"allow_destructive_actions"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	if h.database != nil {
		if err := h.database.UpsertSetting("allow_destructive_actions", payload.Allow, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
			return
		}

		// Send email notification to admins
		if h.emailSender != nil {
			// Would send email here
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// Token management
func (h *AdminHandler) CreateAPIToken(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		UserEmail   string   `json:"user_email"`
		Permissions []string `json:"permissions"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}

	// Send email notification
	if h.emailSender != nil {
		// Would send email here
	}

	c.JSON(http.StatusOK, gin.H{
		"token":       resp.Token,
		"id":          resp.APIToken.ID,
		"name":        resp.APIToken.Name,
		"permissions": resp.APIToken.Permissions,
	})
}

func (h *AdminHandler) ListAPITokens(c *gin.Context) {
	// List all API tokens
	tokens, err := h.database.ListAllAPITokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}

	c.JSON(http.StatusOK, tokens)
}

func (h *AdminHandler) GetAPIToken(c *gin.Context) {
	tokenID, err := strconv.ParseInt(c.Param("tokenID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	token, err := h.database.GetAPITokenByID(tokenID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	c.JSON(http.StatusOK, token)
}

func (h *AdminHandler) UpdateAPIToken(c *gin.Context) {
	tokenID, err := strconv.ParseInt(c.Param("tokenID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := h.database.GetAPITokenByID(tokenID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load token"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
		return
	}

	if name := strings.TrimSpace(req.Name); name != "" && name != existing.Name {
		if err := h.tokenService.UpdateTokenName(tokenID, name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update token name"})
			return
		}
	}

	if req.Permissions != nil {
		if err := h.tokenService.UpdateTokenPermissions(tokenID, req.Permissions); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update token permissions"})
			return
		}
	}

	updated, err := h.database.GetAPITokenByID(tokenID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load token"})
		return
	}
	if updated == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token disappeared"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (h *AdminHandler) DeleteAPIToken(c *gin.Context) {
	tokenID, err := strconv.ParseInt(c.Param("tokenID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token ID"})
		return
	}

	if err := h.database.DeleteAPIToken(tokenID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete token"})
		return
	}

	c.Status(http.StatusNoContent)
}

func collectAllPermissions(rolePerms map[string][]string) []string {
	known := make(map[string]struct{})
	for _, perms := range rolePerms {
		for _, perm := range perms {
			known[perm] = struct{}{}
		}
	}
	for _, perm := range routematch.AllPermissions() {
		known[perm] = struct{}{}
	}
	known["convox:*:*"] = struct{}{}

	perms := make([]string, 0, len(known))
	wildcard := false
	for perm := range known {
		if perm == "convox:*:*" {
			wildcard = true
			continue
		}
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	if wildcard {
		perms = append(perms, "convox:*:*")
	}
	return perms
}

func buildRoleOptions(rolePerms map[string][]string) []roleOption {
	meta := rbac.RoleMetadataMap()
	ordered := rbac.RoleOrder()
	roles := make([]roleOption, 0, len(ordered))
	for _, role := range ordered {
		perms, ok := rolePerms[role]
		if !ok {
			continue
		}
		info, ok := meta[role]
		if !ok {
			continue
		}
		sorted := append([]string(nil), perms...)
		sort.Strings(sorted)
		roles = append(roles, roleOption{
			Name:        role,
			Label:       info.Label,
			Description: info.Description,
			Permissions: sorted,
		})
	}
	return roles
}

func flattenUserRoles(manager rbac.RBACManager, email string, rolePerms map[string][]string) ([]string, []string) {
	if manager == nil || email == "" {
		return nil, nil
	}

	roles, err := manager.GetUserRoles(email)
	if err != nil {
		return nil, nil
	}
	sort.Strings(roles)

	permSet := make(map[string]struct{})
	for _, role := range roles {
		if perms, ok := rolePerms[role]; ok {
			for _, perm := range perms {
				permSet[perm] = struct{}{}
			}
		}
	}

	perms := make([]string, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}
	sort.Strings(perms)

	return roles, perms
}

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

	c.JSON(http.StatusOK, map[string]interface{}{
		"permissions":         allPermissions,
		"roles":               roles,
		"default_permissions": token.DefaultCICDPermissions(),
		"user_roles":          userRoles,
		"user_permissions":    userPerms,
	})
}

func parseAuditTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}

	var lastErr error
	for _, layout := range layouts {
		if layout == "2006-01-02T15:04" || layout == "2006-01-02T15:04:05" {
			if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return t.UTC(), nil
			} else {
				lastErr = err
			}
			continue
		}
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		} else {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unable to parse time %q", value)
	}
	return time.Time{}, lastErr
}

func (h *AdminHandler) auditFiltersFromRequest(c *gin.Context) (db.AuditLogFilters, int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		limit = 100
	}

	userFilter := c.Query("user")
	if userFilter == "" {
		if userIDParam := c.Query("user_id"); userIDParam != "" {
			if userID, convErr := strconv.ParseInt(userIDParam, 10, 64); convErr == nil {
				user, lookupErr := h.database.GetUserByID(userID)
				if lookupErr != nil {
					return db.AuditLogFilters{}, 0, 0, lookupErr
				}
				if user != nil {
					userFilter = user.Email
				}
			}
		}
	}

	statusFilter := c.Query("status")
	actionTypeFilter := c.Query("action_type")
	resourceTypeFilter := c.Query("resource_type")
	searchFilter := c.Query("search")
	rangeFilter := strings.TrimSpace(c.DefaultQuery("range", "24h"))
	startParam := c.Query("start")
	endParam := c.Query("end")
	missingUserForID := false

	var (
		since      time.Time
		until      time.Time
		hasStart   bool
		hasEnd     bool
		startError error
		endError   error
	)

	if strings.TrimSpace(startParam) != "" {
		parsed, parseErr := parseAuditTime(startParam)
		if parseErr != nil {
			startError = parseErr
		} else {
			since = parsed
			hasStart = true
		}
	}
	if strings.TrimSpace(endParam) != "" {
		parsed, parseErr := parseAuditTime(endParam)
		if parseErr != nil {
			endError = parseErr
		} else {
			until = parsed
			hasEnd = true
		}
	}

	if startError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidStartTime
	}
	if endError != nil {
		return db.AuditLogFilters{}, 0, 0, errInvalidEndTime
	}
	if hasStart && hasEnd && until.Before(since) {
		return db.AuditLogFilters{}, 0, 0, errInvalidTimeRange
	}

	if !hasStart {
		now := time.Now()
		switch rangeFilter {
		case "15m":
			since = now.Add(-15 * time.Minute)
			hasStart = true
		case "1h":
			since = now.Add(-1 * time.Hour)
			hasStart = true
		case "24h":
			since = now.Add(-24 * time.Hour)
			hasStart = true
		case "7d":
			since = now.Add(-7 * 24 * time.Hour)
			hasStart = true
		case "30d":
			since = now.Add(-30 * 24 * time.Hour)
			hasStart = true
		case "all":
			// no lower bound
		case "custom":
			// rely on explicit start/end parameters
		default:
			// fallback to 24h
			since = now.Add(-24 * time.Hour)
			hasStart = true
		}
	} else {
		// Ensure "custom" is reflected for URL sync if explicit start is provided without range
		if rangeFilter == "" {
			rangeFilter = "custom"
		}
	}

	resolvedUserEmail := userFilter
	if userFilter == "" && c.Query("user_id") != "" {
		missingUserForID = true
	}

	filters := db.AuditLogFilters{
		UserEmail:    resolvedUserEmail,
		Status:       statusFilter,
		ActionType:   actionTypeFilter,
		ResourceType: resourceTypeFilter,
		Search:       searchFilter,
		Limit:        limit,
		Offset:       (page - 1) * limit,
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}
	if hasStart {
		filters.Since = since
	}
	if hasEnd {
		filters.Until = until
	}
	if missingUserForID {
		filters.UserEmail = fmt.Sprintf("__missing_user_%s__", strings.TrimSpace(c.Query("user_id")))
	}

	return filters, page, limit, nil
}
