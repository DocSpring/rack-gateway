package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Each approved deploy command stored as a string becomes an object with just its command, in the same order. An
// entry that's already an object, other settings and values that aren't lists are left alone.
func TestApprovedDeployCommandsMigrationMakesEachCommandAnObject(t *testing.T) {
	database := dbtest.NewDatabase(t)
	web, worker, empty, odd := "web", "worker", "empty", "odd"
	require.NoError(t, database.UpsertSetting(&web, "approved_deploy_commands",
		[]interface{}{
			"bin/migrate",
			map[string]interface{}{"command": "rake stripe:prepare", "write": true},
			`bin/rails "db:seed"`,
		}, nil))
	require.NoError(t, database.UpsertSetting(&worker, "protected_env_vars", []string{"DATABASE_URL"}, nil))
	require.NoError(t, database.UpsertSetting(&empty, "approved_deploy_commands", []string{}, nil))
	require.NoError(t, database.UpsertSetting(&odd, "approved_deploy_commands", "bin/migrate", nil))

	for range 2 {
		require.NoError(t, database.RunMigrationForTest("20261026000000_approved_deploy_command_objects.sql"))
	}

	read := func(app, key string) string {
		raw, found, err := database.GetSetting(&app, key)
		require.NoError(t, err)
		require.True(t, found)
		return string(raw)
	}
	require.JSONEq(t, `[{"command":"bin/migrate"},{"command":"rake stripe:prepare","write":true},`+
		`{"command":"bin/rails \"db:seed\""}]`, read(web, "approved_deploy_commands"))
	require.JSONEq(t, `["DATABASE_URL"]`, read(worker, "protected_env_vars"))
	require.JSONEq(t, `[]`, read(empty, "approved_deploy_commands"))
	require.JSONEq(t, `"bin/migrate"`, read(odd, "approved_deploy_commands"))
}
