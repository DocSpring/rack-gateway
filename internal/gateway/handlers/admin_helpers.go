package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// AdminHandler is defined in admin.go
// RoleDescriptor is defined in dto.go

func (h *AdminHandler) rackDisplay() string {
	return rackDisplay(h.config)
}

func (h *AdminHandler) publicBaseURL(c *gin.Context) string {
	if h != nil && h.config != nil {
		if url := normalizeConfigDomain(h.config.Domain); url != "" {
			return url
		}
	}
	if c != nil && c.Request != nil {
		return buildURLFromRequest(c.Request)
	}
	return ""
}

func normalizeConfigDomain(domain string) string {
	raw := strings.TrimSpace(domain)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.Contains(raw, "localhost") || strings.Contains(raw, ":") {
		return "http://" + raw
	}
	return "https://" + raw
}

func buildURLFromRequest(req *http.Request) string {
	scheme := detectRequestScheme(req)
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func detectRequestScheme(req *http.Request) string {
	if proto := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if req.TLS == nil {
		return "http"
	}
	return "https"
}

// TriggerSentryTest manually sends a test event to Sentry for verification purposes.
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

func (_ *AdminHandler) currentAuthUser(c *gin.Context) *auth.User {
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
	return &auth.User{Email: email, Name: name}
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
	clone := make(map[string]interface{}, len(details))
	for k, v := range details {
		clone[k] = v
	}
	return clone
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

func flattenUserRoles(manager rbac.Manager, email string, rolePerms map[string][]string) ([]string, []string) {
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
			t, err := time.ParseInLocation(layout, value, time.Local)
			if err == nil {
				return t.UTC(), nil
			}
			lastErr = err
			continue
		}
		t, err := time.Parse(layout, value)
		if err == nil {
			return t.UTC(), nil
		}
		lastErr = err
	}

	if lastErr == nil {
		return time.Time{}, fmt.Errorf("unable to parse time %q", value)
	}
	return time.Time{}, lastErr
}
