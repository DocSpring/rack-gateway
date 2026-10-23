package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Only finished failures count toward the automatic lock: an attempt still being verified hasn't failed, and a
// rate-limited one verified nothing.
func TestCountRecentFailedMFAAttemptsCountsFinishedFailures(t *testing.T) {
	database, user := newEnrollmentUser(t)
	req := gwdb.MFAAttemptRequest{UserID: user.ID, MethodType: gwdb.MFAMethodTypeTOTP}
	reserve := func() *gwdb.MFAAttempt {
		t.Helper()
		attempt, err := database.ReserveMFAAttempt(req, 10, 5)
		require.NoError(t, err)
		return attempt
	}
	failedCount := func() int {
		t.Helper()
		count, err := database.CountRecentFailedMFAAttempts(user.ID, 5)
		require.NoError(t, err)
		return count
	}

	reserve() // still being verified
	require.Zero(t, failedCount())
	require.NoError(t, reserve().Finish(nil, false, "invalid_code"))
	require.NoError(t, reserve().Finish(nil, false, ""))
	require.NoError(t, reserve().Finish(nil, true, ""))
	require.Equal(t, 2, failedCount())

	_, err := database.ReserveMFAAttempt(req, 4, 5)
	require.ErrorIs(t, err, gwdb.ErrMFAAttemptLimit)
	require.Equal(t, 2, failedCount(), "a rate-limited attempt isn't a failed verification")
}
