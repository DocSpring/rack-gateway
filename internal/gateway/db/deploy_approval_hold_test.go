package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// holdApproval holds the request (WithDeployApprovalRequestHeld) until the returned function is called. It returns
// the status the request was held with.
func holdApproval(t *testing.T, database *gwdb.Database, id int64) (string, func()) {
	t.Helper()
	held := make(chan string)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- database.WithDeployApprovalRequestHeld(id, func(request *gwdb.DeployApprovalRequest) error {
			held <- request.Status
			<-release
			return nil
		})
	}()
	select {
	case status := <-held:
		return status, func() {
			close(release)
			require.NoError(t, <-done)
		}
	case err := <-done:
		t.Fatalf("couldn't hold deploy approval request %d: %v", id, err)
		return "", nil
	}
}

// A rejection waits while the approval is held, so whoever holds it (the CircleCI approval job) acts on the status
// it was given.
func TestARejectionWaitsForAHeldApproval(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(request.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)

	status, release := holdApproval(t, database, request.ID)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, status)
	rejected := make(chan error, 1)
	go func() {
		_, err := database.RejectDeployApprovalRequest(request.ID, userID, "")
		rejected <- err
	}()
	select {
	case err := <-rejected:
		t.Fatalf("the rejection didn't wait for the held approval: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	release()
	require.NoError(t, <-rejected)
	reloaded, err := database.GetDeployApprovalRequest(request.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusRejected, reloaded.Status)
}

// The expiry sweep holds the audit chain lock, so it skips a held approval rather than waiting for it, and
// expires it on a later sweep.
func TestTheExpirySweepSkipsAHeldApproval(t *testing.T) {
	database, request := approvedPastItsWindow(t)

	_, release := holdApproval(t, database, request.ID)
	expired, _, err := database.ExpireDeployApprovalRequests(0, expiryEvent)
	require.NoError(t, err)
	require.Empty(t, expired)

	release()
	expired, _, err = database.ExpireDeployApprovalRequests(0, expiryEvent)
	require.NoError(t, err)
	require.Len(t, expired, 1)
}

func TestHoldingAMissingApprovalFails(t *testing.T) {
	database, _, _ := newApprovalTestFixture(t)
	err := database.WithDeployApprovalRequestHeld(404, func(*gwdb.DeployApprovalRequest) error {
		t.Fatal("fn is only called with a request")
		return nil
	})
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound)
}
