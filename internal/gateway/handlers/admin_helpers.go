package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	sentry "github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

func (h *AdminHandler) rackDisplay() string {
	if h == nil || h.config == nil {
		return "Convox Rack"
	}
	preferred := []string{"default", "local"}
	for _, key := range preferred {
		if rc, ok := h.config.Racks[key]; ok && rc.Enabled {
			if alias := strings.TrimSpace(rc.Alias); alias != "" {
				return alias
			}
			if name := strings.TrimSpace(rc.Name); name != "" {
				return name
			}
		}
	}
	for _, rc := range h.config.Racks {
		if !rc.Enabled {
			continue
		}
		if alias := strings.TrimSpace(rc.Alias); alias != "" {
			return alias
		}
		if name := strings.TrimSpace(rc.Name); name != "" {
			return name
		}
	}
	return "Convox Rack"
}

func (h *AdminHandler) publicBaseURL(c *gin.Context) string {
	if h != nil && h.config != nil {
		if raw := strings.TrimSpace(h.config.Domain); raw != "" {
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				return raw
			}
			if strings.Contains(raw, "localhost") || strings.Contains(raw, ":") {
				return "http://" + raw
			}
			return "https://" + raw
		}
	}
	if c != nil && c.Request != nil {
		scheme := "https"
		if proto := strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = proto
		} else if c.Request.TLS == nil {
			scheme = "http"
		}
		host := strings.TrimSpace(c.Request.Host)
		if host != "" {
			return fmt.Sprintf("%s://%s", scheme, host)
		}
	}
	return ""
}

func (h *AdminHandler) TriggerSentryTest(c *gin.Context) {
	var payload struct {
		Kind string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	kind := strings.TrimSpace(strings.ToLower(payload.Kind))
	if kind == "" {
		kind = "api"
	}

	switch kind {
	case "api":
		hub := sentrygin.GetHubFromContext(c)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}
		if hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("trigger", "admin-api")
				scope.SetTag("test", "sentry-api")
				scope.SetExtra("triggered_at", time.Now().UTC().Format(time.RFC3339))
				if user := h.currentAuthUser(c); user != nil {
					scope.SetUser(sentry.User{Email: user.Email, Username: user.Name})
				}
				hub.CaptureMessage("Sentry API test event requested via settings page")
			})
		}
		c.JSON(http.StatusOK, gin.H{"status": "captured"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported test kind"})
	}
}

func (h *AdminHandler) currentAuthUser(c *gin.Context) *auth.AuthUser {
	if c == nil || c.Request == nil {
		return nil
	}
	if user, ok := auth.GetAuthUser(c.Request.Context()); ok && user != nil {
		return user
	}
	email := strings.TrimSpace(c.GetString("user_email"))
	name := strings.TrimSpace(c.GetString("user_name"))
	if email == "" {
		return nil
	}
	return &auth.AuthUser{Email: email, Name: name}
}

func (h *AdminHandler) getAdminEmails() []string {
	if h == nil || h.rbac == nil {
		return nil
	}
	users, err := h.rbac.GetUsers()
	if err != nil {
		return nil
	}
	emails := make([]string, 0)
	for email, user := range users {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		for _, role := range user.Roles {
			if role == "admin" {
				emails = append(emails, email)
				break
			}
		}
	}
	if len(emails) == 0 {
		return nil
	}
	sort.Strings(emails)
	return emails
}

func cloneDetails(details map[string]interface{}) map[string]interface{} {
	if len(details) == 0 {
		return nil
	}
	copy := make(map[string]interface{}, len(details))
	for k, v := range details {
		copy[k] = v
	}
	return copy
}

func prioritiseInviterFirst(admins []string, inviterEmail string) []string {
	if inviterEmail == "" || len(admins) == 0 {
		return admins
	}
	idx := -1
	for i, addr := range admins {
		if strings.EqualFold(addr, inviterEmail) {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return admins
	}
	reordered := make([]string, 0, len(admins))
	reordered = append(reordered, admins[idx])
	for i, addr := range admins {
		if i == idx {
			continue
		}
		reordered = append(reordered, addr)
	}
	return reordered
}

func collectAllPermissions(rolePerms map[string][]string) []string {
	known := make(map[string]struct{})
	for _, perms := range rolePerms {
		for _, perm := range perms {
			known[perm] = struct{}{}
		}
	}
	for _, perm := range rbac.RackAllPermissions() {
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

func buildRoleOptions(rolePerms map[string][]string) []RoleDescriptor {
	meta := rbac.RoleMetadataMap()
	ordered := rbac.RoleOrder()
	roles := make([]RoleDescriptor, 0, len(ordered))
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
		roles = append(roles, RoleDescriptor{
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
