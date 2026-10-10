package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Seeded users have no placeholder name, and seeding again keeps the name a user has since been given.
func TestSeedDatabaseLeavesNamesToUsers(t *testing.T) {
	database := dbtest.NewDatabase(t)
	seed := &gwdb.SeedConfig{
		AdminUsers:  []string{"admin@example.com"},
		ViewerUsers: []string{"viewer@example.com"},
	}
	require.NoError(t, database.SeedDatabase(seed))

	admin, err := database.GetUser("admin@example.com")
	require.NoError(t, err)
	require.Empty(t, admin.Name)
	require.Equal(t, []string{"admin"}, admin.Roles)

	require.NoError(t, database.UpdateUserName("admin@example.com", "Real Name"))
	require.NoError(t, database.SeedDatabase(seed))

	admin, err = database.GetUser("admin@example.com")
	require.NoError(t, err)
	require.Equal(t, "Real Name", admin.Name)
	viewer, err := database.GetUser("viewer@example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, viewer.Roles)
}
