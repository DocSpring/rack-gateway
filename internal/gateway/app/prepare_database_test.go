package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A dev build pointed at a production database refuses it before it migrates it.
func TestPrepareDatabaseRefusesAProductionDatabaseBeforeMigratingIt(t *testing.T) {
	database := dbtest.NewDatabase(t)
	require.NoError(t, database.SetEnvironment(false))
	var pending string
	require.NoError(t, database.DB().QueryRow(`DELETE FROM schema_migrations
		WHERE version = (SELECT max(version) FROM schema_migrations) RETURNING version`).Scan(&pending))

	err := prepareDatabase(database, true, fakeEnv(map[string]string{"DEV_MODE": "true"}))
	require.ErrorContains(t, err, "refusing to start")

	var applied int
	require.NoError(t, database.DB().QueryRow(
		`SELECT count(*) FROM schema_migrations WHERE version = $1`, pending).Scan(&applied))
	require.Zero(t, applied, "the pending migration %s ran", pending)
}

// A database without an environment marker is checked as the environment the gateway would mark it: a gateway
// that isn't in dev mode marks it production, so it can't start on it with a test bypass set.
func TestPrepareDatabaseChecksAnUnmarkedDatabaseAsTheEnvironmentItWouldGet(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.DB().Exec(`DELETE FROM rgw_internal_metadata`)
	require.NoError(t, err)

	err = prepareDatabase(database, false, fakeEnv(map[string]string{"E2E_TEST_MODE": "true"}))
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "E2E_TEST_MODE"), err.Error())
	environment, err := database.CurrentEnvironment()
	require.NoError(t, err)
	require.Empty(t, environment)

	require.NoError(t, prepareDatabase(database, false, fakeEnv(nil)))
	environment, err = database.CurrentEnvironment()
	require.NoError(t, err)
	require.Equal(t, "production", environment)
}
