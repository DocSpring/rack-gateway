package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A reset clears the job queue too: a job queued for the old database must not run against the new one, where its
// record IDs belong to other records.
func TestResetDatabaseClearsQueuedJobs(t *testing.T) {
	t.Setenv("RESET_RACK_GATEWAY_DATABASE", "DELETE_ALL_DATA")
	t.Setenv("DISABLE_DATABASE_ENVIRONMENT_CHECK", "1")
	database := dbtest.NewDatabase(t)
	_, err := database.DB().Exec(`INSERT INTO river_job (args, kind, max_attempts) VALUES ('{}', 'stale_job', 1)`)
	require.NoError(t, err)

	require.NoError(t, database.ResetDatabase())

	var queued int
	require.NoError(t, database.DB().QueryRow(`SELECT count(*) FROM river_job`).Scan(&queued))
	require.Zero(t, queued)
}
