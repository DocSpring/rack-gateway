package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A checked build is listed for its app, with its ID once the rack has named it, until it's finished or older than
// CheckedBuildMaxAge.
func TestCheckedBuilds(t *testing.T) {
	database := dbtest.NewDatabase(t)

	first, err := database.RecordCheckedBuild("web")
	require.NoError(t, err)
	second, err := database.RecordCheckedBuild("web")
	require.NoError(t, err)
	_, err = database.RecordCheckedBuild("worker")
	require.NoError(t, err)
	require.NoError(t, database.SetCheckedBuildID(second, "B2"))

	builds, err := database.UnfinishedCheckedBuilds("web")
	require.NoError(t, err)
	require.Len(t, builds, 2)
	require.Equal(t, first, builds[0].ID)
	require.Empty(t, builds[0].BuildID, "the rack hasn't named it yet")
	require.Equal(t, "B2", builds[1].BuildID)

	require.NoError(t, database.FinishCheckedBuild(second))
	_, err = database.DB().Exec(`UPDATE checked_builds SET checked_at = NOW() - INTERVAL '61 minutes' WHERE id = $1`,
		first)
	require.NoError(t, err)
	builds, err = database.UnfinishedCheckedBuilds("web")
	require.NoError(t, err)
	require.Empty(t, builds, "a build older than an hour has read its env")

	builds, err = database.UnfinishedCheckedBuilds("worker")
	require.NoError(t, err)
	require.Len(t, builds, 1)
}
