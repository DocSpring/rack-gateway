package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test WelcomeArgs.Kind
func TestWelcomeArgs_Kind(t *testing.T) {
	args := WelcomeArgs{
		Email:        "newuser@example.com",
		Name:         "New User",
		Roles:        []string{"viewer", "deployer"},
		InviterEmail: "admin@example.com",
		Rack:         "production",
		BaseURL:      "https://gateway.example.com",
	}
	assert.Equal(t, "email:user:welcome", args.Kind())
}

// Test NewWelcomeWorker
func TestNewWelcomeWorker(t *testing.T) {
	worker := NewWelcomeWorker(nil)
	require.NotNil(t, worker)
}

// Test UserAddedAdminArgs.Kind
func TestUserAddedAdminArgs_Kind(t *testing.T) {
	args := UserAddedAdminArgs{
		AdminEmails:  []string{"admin@example.com"},
		NewUserEmail: "newuser@example.com",
		NewUserName:  "New User",
		Roles:        []string{"viewer", "deployer"},
		CreatorEmail: "creator@example.com",
		Rack:         "production",
	}
	assert.Equal(t, "email:user:added_admin", args.Kind())
}

// Test NewUserAddedAdminWorker
func TestNewUserAddedAdminWorker(t *testing.T) {
	worker := NewUserAddedAdminWorker(nil)
	require.NotNil(t, worker)
}
