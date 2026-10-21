package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func seedLists() *gwdb.SeedConfig {
	return &gwdb.SeedConfig{
		AdminUsers:      []string{"Admin@example.com"},
		ViewerUsers:     []string{"viewer@example.com"},
		DeployerUsers:   []string{"both@example.com"},
		OperationsUsers: []string{"both@example.com"},
	}
}

func requireRoles(t *testing.T, database *gwdb.Database, email string, roles ...string) *gwdb.User {
	t.Helper()
	user, err := database.GetUser(email)
	require.NoError(t, err)
	require.NotNil(t, user, email)
	require.Equal(t, roles, user.Roles, email)
	return user
}

// Seeding creates the listed users that don't exist, each with the roles of every list that names it and no name,
// and records each creation by the system.
func TestSeedDatabaseCreatesMissingUsersAndAuditsThem(t *testing.T) {
	database := dbtest.NewDatabase(t)

	events, err := database.SeedDatabase(seedLists(), audit.SeededUserEntry)
	require.NoError(t, err)
	require.Len(t, events, 3)

	require.Empty(t, requireRoles(t, database, "admin@example.com", "admin").Name)
	requireRoles(t, database, "viewer@example.com", "viewer")
	requireRoles(t, database, "both@example.com", "deployer", "ops")

	logs, err := database.GetAuditLogs(audit.SystemActor, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	resources := make([]string, 0, len(logs))
	for _, log := range logs {
		require.Equal(t, "user.create", log.Action)
		require.Equal(t, "success", log.Status)
		resources = append(resources, log.Resource)
	}
	require.ElementsMatch(t, []string{"admin@example.com", "viewer@example.com", "both@example.com"}, resources)
}

// Seeding never changes an existing user: a role or name given since stays, and nothing new is recorded.
func TestSeedDatabaseLeavesExistingUsersAlone(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.SeedDatabase(seedLists(), audit.SeededUserEntry)
	require.NoError(t, err)

	admin, err := database.GetUser("admin@example.com")
	require.NoError(t, err)
	name := "Real Name"
	_, err = database.UpdateUser(admin.ID, gwdb.UserUpdate{Roles: []string{"viewer"}, Name: &name}, nil)
	require.NoError(t, err)

	events, err := database.SeedDatabase(seedLists(), audit.SeededUserEntry)
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, "Real Name", requireRoles(t, database, "admin@example.com", "viewer").Name)
}

// A user is never created without its audit event: when the event can't be appended, no user is created.
func TestSeedDatabaseCreatesNothingWithoutItsAuditEvent(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.DB().Exec(`ALTER FUNCTION audit.append_audit_event RENAME TO append_audit_event_unavailable`)
	require.NoError(t, err)

	events, err := database.SeedDatabase(seedLists(), audit.SeededUserEntry)
	require.ErrorIs(t, err, gwdb.ErrAuditAppend)
	require.Empty(t, events)

	user, err := database.GetUser("admin@example.com")
	require.NoError(t, err)
	require.Nil(t, user)
}
