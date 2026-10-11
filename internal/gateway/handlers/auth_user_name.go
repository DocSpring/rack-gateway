package handlers

import (
	"log"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// maxUserNameLength matches users.name (VARCHAR(120)).
const maxUserNameLength = 120

// fillUserName gives a user without a name (e.g. one seeded from ADMIN_USERS) the name from the identity
// provider when they sign in. A name set in the gateway is never overwritten.
func (h *AuthHandler) fillUserName(user *db.User, providerName string) {
	name := strings.TrimSpace(providerName)
	if user == nil || strings.TrimSpace(user.Name) != "" || name == "" || len([]rune(name)) > maxUserNameLength {
		return
	}
	filled, err := h.database.FillUserName(user.Email, name)
	if err != nil {
		log.Printf("failed to set name for %s from the identity provider: %v", user.Email, err)
		return
	}
	if filled {
		user.Name = name
	}
}
