package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// An object key is claimed once, by the first upload or the first build that checks it, and never released.
func TestObjectKeysAreClaimedOnce(t *testing.T) {
	database := dbtest.NewDatabase(t)

	claimed, err := database.ClaimObjectUpload("web", "tmp/a.tgz")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = database.ClaimObjectUpload("web", "tmp/a.tgz")
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = database.ClaimObjectUpload("worker", "tmp/a.tgz")
	require.NoError(t, err)
	require.True(t, claimed, "each app has its own objects")

	require.ErrorIs(t, database.ClaimObjectForBuild("web", "tmp/a.tgz"), gwdb.ErrObjectUploadInProgress)
	require.NoError(t, database.MarkObjectUploaded("web", "tmp/a.tgz"))
	require.NoError(t, database.ClaimObjectForBuild("web", "tmp/a.tgz"))

	// An object the gateway never saw uploaded is claimed by the build that checks it.
	require.NoError(t, database.ClaimObjectForBuild("web", "tmp/older.tgz"))
	claimed, err = database.ClaimObjectUpload("web", "tmp/older.tgz")
	require.NoError(t, err)
	require.False(t, claimed)
}
