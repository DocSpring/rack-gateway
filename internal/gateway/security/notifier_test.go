package security

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Alerts go to the admins at the time of the alert: one added after startup gets them, and a locked one doesn't.
func TestNotifierLooksUpAdminsWhenAlerting(t *testing.T) {
	database := dbtest.NewDatabase(t)
	n := NewNotifier(nil, database, nil)
	t.Cleanup(n.Stop)
	require.Empty(t, n.admins())

	_, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	locked, err := database.CreateUser("locked@example.com", "Locked", []string{"admin"})
	require.NoError(t, err)
	require.NoError(t, database.LockUser(locked.ID, "away", nil))
	_, err = database.CreateUser("viewer@example.com", "Viewer", []string{"viewer"})
	require.NoError(t, err)

	require.Equal(t, []string{"admin@example.com"}, n.admins())
}

// flakyJobs fails its first failures inserts, then queues.
type flakyJobs struct {
	mu       sync.Mutex
	failures int
	queued   []river.JobArgs
}

func (j *flakyJobs) Insert(
	_ context.Context,
	args river.JobArgs,
	_ *river.InsertOpts,
) (*rivertype.JobInsertResult, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failures > 0 {
		j.failures--
		return nil, errors.New("queue unavailable")
	}
	j.queued = append(j.queued, args)
	return &rivertype.JobInsertResult{}, nil
}

// An alert only counts as sent once its email is queued: after a failed insert the next alert tries again, and once
// one is queued the same alert is held back for the dedup window.
func TestAFailedInsertDoesntHoldBackTheNextAlert(t *testing.T) {
	n := NewNotifier(nil, nil, nil)
	t.Cleanup(n.Stop)
	inserter := &flakyJobs{failures: 1}
	n.jobs = inserter
	alert := func() { n.FailedMFAAttempt("user@example.com", "User", "203.0.113.5", "test") }

	alert()
	require.Empty(t, inserter.queued)
	alert()
	require.Len(t, inserter.queued, 1, "the alert after the failed insert is sent")
	alert()
	require.Len(t, inserter.queued, 1, "the same alert within the dedup window isn't")
}
