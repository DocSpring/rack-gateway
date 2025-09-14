//go:build !dev

package ui

import "github.com/go-chi/chi/v5"

// RegisterDevAdminRoutes is a no-op in non-dev builds.
func RegisterDevAdminRoutes(r chi.Router, h *Handler) {}
