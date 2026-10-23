package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Deleting a user removes them and the record of who created them, and deleting one that doesn't exist fails.
func TestDeleteUser(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	target, err := database.CreateUser("target@example.com", "Target", []string{"viewer"})
	require.NoError(t, err)
	_, err = database.CreateUserResource(admin.ID, "user", target.Email)
	require.NoError(t, err)

	require.NoError(t, database.DeleteUser(target.Email))
	deleted, err := database.GetUserByID(target.ID)
	require.NoError(t, err)
	require.Nil(t, deleted)
	require.ErrorIs(t, database.DeleteUser(target.Email), gwdb.ErrUserNotFound)

	// A user created later with the same email has no creator until one is recorded.
	_, err = database.CreateUser(target.Email, "New", []string{"viewer"})
	require.NoError(t, err)
	users, err := database.ListUsers()
	require.NoError(t, err)
	for _, user := range users {
		if user.Email == target.Email {
			require.Nil(t, user.CreatedByUserID)
		}
	}
}
