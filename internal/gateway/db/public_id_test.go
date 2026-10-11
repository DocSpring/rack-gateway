package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A malformed public ID (e.g. a numeric id) is "not found", not a database error that would become a 500.
func TestMalformedPublicIDsAreNotFound(t *testing.T) {
	database := dbtest.NewDatabase(t)

	const valid = "406e7893-a0d8-4b80-8ff7-2d6b8e1c5a70"
	malformed := []string{
		"4", "not-a-uuid", "406e7893-a0d8-4b80-8ff7",
		// uuid.Parse accepts these, but Postgres doesn't.
		"urn:uuid:" + valid, "x" + valid + "x",
	}
	for _, id := range malformed {
		token, err := database.GetAPITokenByPublicID(id)
		require.NoError(t, err, id)
		require.Nil(t, token, id)

		_, err = database.GetDeployApprovalRequestByPublicID(id)
		require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound, id)

		_, err = database.ApproveDeployApprovalRequestByPublicID(id, 1, time.Now().Add(time.Hour), "")
		require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound, id)

		_, err = database.RejectDeployApprovalRequestByPublicID(id, 1, "")
		require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound, id)

		_, err = database.ExtendDeployApprovalRequestExpiry(id, time.Now().Add(time.Hour))
		require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound, id)
	}
}
