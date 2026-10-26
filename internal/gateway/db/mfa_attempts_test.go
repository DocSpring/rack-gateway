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

	// The rate limit counts the unfinished attempt and the two failures, not the success.
	_, err := database.ReserveMFAAttempt(req, 3, 5)
	require.ErrorIs(t, err, gwdb.ErrMFAAttemptLimit)
	require.Equal(t, 2, failedCount(), "a rate-limited attempt isn't a failed verification")
}

// The rate limit counts attempts that failed or are still being verified. Successful attempts don't count: every
// command asks for a fresh proof, so a person running a few commands, or approving a few deploys, verifies many
// times in five minutes. Refused attempts don't count either, or retrying would keep the limit in force.
func TestReserveMFAAttemptCountsOnlyFailedAndUnfinishedAttempts(t *testing.T) {
	database, user := newEnrollmentUser(t)
	req := gwdb.MFAAttemptRequest{UserID: user.ID, MethodType: gwdb.MFAMethodTypeWebAuthn}
	const limit = 3
	reserve := func() (*gwdb.MFAAttempt, error) {
		t.Helper()
		return database.ReserveMFAAttempt(req, limit, 5)
	}

	for range 10 {
		attempt, err := reserve()
		require.NoError(t, err, "successful attempts don't count toward the limit")
		require.NoError(t, attempt.Finish(nil, true, ""))
	}

	unfinished, err := reserve()
	require.NoError(t, err)
	failed, err := reserve()
	require.NoError(t, err)
	require.NoError(t, failed.Finish(nil, false, "invalid_signature"))
	_, err = database.ReserveMFAAttempt(
		gwdb.MFAAttemptRequest{UserID: user.ID, MethodType: gwdb.MFAMethodTypeTOTP}, 1, 5,
	)
	require.NoError(t, err, "each kind of factor has its own limit")

	last, err := reserve()
	require.NoError(t, err)
	for range 3 {
		_, err = reserve()
		require.ErrorIs(t, err, gwdb.ErrMFAAttemptLimit, "an unfinished and two failed attempts reach the limit")
	}

	// The refused attempts didn't count: once the unfinished ones succeed, there's room again.
	require.NoError(t, unfinished.Finish(nil, true, ""))
	require.NoError(t, last.Finish(nil, true, ""))
	_, err = reserve()
	require.NoError(t, err)
}
