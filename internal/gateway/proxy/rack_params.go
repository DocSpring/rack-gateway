package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func (h *Handler) fetchSystemParams(ctx context.Context, rack config.RackConfig) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	targetURL := base + "/system"
	req, err := http.NewRequest(http.MethodGet, targetURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set(
		"Authorization",
		fmt.Sprintf(
			"Basic %s",
			base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey))),
		),
	)
	client, err := h.httpClient(ctx, 15*time.Second)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if fpErr, ok := rackcert.AsFingerprintMismatch(err); ok {
			logRackTLSMismatch("fetch_system_params", fpErr)
			return nil, fpErr
		}
		return nil, err
	}
	//nolint:errcheck,gosec // G104: response cleanup
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var payload struct {
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRackJSONBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Parameters == nil {
		payload.Parameters = map[string]string{}
	}
	// Copy
	out := make(map[string]string, len(payload.Parameters))
	for k, v := range payload.Parameters {
		out[k] = v
	}
	return out, nil
}

// changedParams returns the names of the rack parameters whose values differ, sorted.
func changedParams(before, after map[string]string) []string {
	if after == nil {
		return nil
	}
	keys := map[string]struct{}{}
	for k := range after {
		keys[k] = struct{}{}
	}
	for k := range before {
		keys[k] = struct{}{}
	}
	changed := make([]string, 0, len(keys))
	for k := range keys {
		if before[k] != after[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// Rack parameter values can hold credentials (a database password, a private key), so the admin email and the
// audit log name the changed parameters and never their values.

// notifyRackParamsChanged emails the admins the names of the changed rack parameters.
func (h *Handler) notifyRackParamsChanged(actor string, keys []string) {
	if h.rbacManager == nil || len(keys) == 0 {
		return
	}
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	subject := actor + " changed rack parameters"
	text, html, _ := emailtemplates.RenderRackParamsChanged(h.rackDisplay(), actor, strings.Join(keys, "\n"))
	_ = h.emailer.SendMany(admins, subject, text, html)
}

// auditRackParamsChanged logs the changed rack parameters, with their values redacted.
func (h *Handler) auditRackParamsChanged(r *http.Request, actor string, keys []string) {
	if h.database == nil || len(keys) == 0 {
		return
	}
	redacted := map[string]string{"old": envutil.RedactedValue, "new": envutil.RedactedValue}
	changes := make(map[string]map[string]string, len(keys))
	for _, key := range keys {
		changes[key] = redacted
	}
	b, _ := json.Marshal(map[string]interface{}{"changes": changes})
	_ = h.logAudit(r, &db.AuditLog{
		UserEmail:    actor,
		UserName:     r.Header.Get("X-User-Name"),
		ActionType:   "convox",
		Action:       audit.BuildAction(rbac.ResourceRack.String(), audit.ActionVerbParamsSet),
		ResourceType: "rack",
		Resource:     h.rackName,
		Details:      string(b),
		IPAddress:    netutil.ClientIP(r),
		UserAgent:    r.UserAgent(),
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})
}

func (h *Handler) getAdminEmails() []string {
	if h.rbacManager == nil {
		return nil
	}
	users, err := h.rbacManager.GetUsers()
	if err != nil {
		return nil
	}
	emails := make([]string, 0)
	for emailAddr, u := range users {
		for _, r := range u.Roles {
			if r == "admin" {
				emails = append(emails, emailAddr)
				break
			}
		}
	}
	return emails
}
