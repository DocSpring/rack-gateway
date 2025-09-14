//go:build dev

package ui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/DocSpring/convox-gateway/internal/gateway/email"
)

// DevListEmails returns recent dev outbox emails (LoggerSender), admin-only.
func (h *Handler) DevListEmails(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Optional ?limit=N
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	out := email.GetDevOutbox(limit)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
