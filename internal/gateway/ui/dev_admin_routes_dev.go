//go:build dev

package ui

import "github.com/go-chi/chi/v5"

// RegisterDevAdminRoutes registers dev-only admin routes. Included with -tags=dev.
func RegisterDevAdminRoutes(r chi.Router, h *Handler) {
	r.Get("/dev/emails", h.DevListEmails)
}
