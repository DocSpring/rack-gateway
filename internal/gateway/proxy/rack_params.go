package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type paramChange struct {
	Key string
	Old string
	New string
}

func (h *Handler) fetchSystemParams(ctx context.Context, rack config.RackConfig) (map[string]string, error) {
	base := strings.TrimRight(rack.URL, "/")
	targetURL := base + "/system"
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", rack.Username, rack.APIKey)))))
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
	defer resp.Body.Close() //nolint:errcheck // response cleanup
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var payload struct {
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
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

func diffParams(before, after map[string]string) []paramChange {
	changes := []paramChange{}
	if after == nil {
		return changes
	}
	// include keys from both maps
	keys := map[string]struct{}{}
	for k := range after {
		keys[k] = struct{}{}
	}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range keys {
		ov := before[k]
		nv := after[k]
		if ov != nv {
			changes = append(changes, paramChange{Key: k, Old: ov, New: nv})
		}
	}
	return changes
}

func (h *Handler) notifyRackParamsChanged(_ *http.Request, actor string, changes []paramChange) {
	if h.rbacManager == nil || len(changes) == 0 {
		return
	}
	admins := h.getAdminEmails()
	if len(admins) == 0 {
		return
	}
	// Build value string listing changes
	var b strings.Builder
	for i, c := range changes {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s -> %s", c.Key, c.Old, c.New)
	}
	subject := fmt.Sprintf("Rack Gateway (%s): %s changed rack parameters", h.rackDisplay(), actor)
	text, html, _ := emailtemplates.RenderRackParamsChanged(h.rackDisplay(), actor, b.String())
	_ = h.emailer.SendMany(admins, subject, text, html)
}

func (h *Handler) auditRackParamsChanged(r *http.Request, actor string, changes []paramChange) {
	if h.database == nil || len(changes) == 0 {
		return
	}
	// Build details JSON
	payload := map[string]interface{}{"changes": func() map[string]map[string]string {
		m := map[string]map[string]string{}
		for _, c := range changes {
			m[c.Key] = map[string]string{"old": c.Old, "new": c.New}
		}
		return m
	}()}
	b, _ := json.Marshal(payload)
	_ = h.logAudit(r, &db.AuditLog{
		UserEmail:    actor,
		UserName:     r.Header.Get("X-User-Name"),
		ActionType:   "convox",
		Action:       audit.BuildAction(rbac.ResourceRack.String(), audit.ActionVerbParamsSet),
		ResourceType: "rack",
		Resource:     h.rackName,
		Details:      string(b),
		IPAddress:    clientIPFromRequest(r),
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
