package db_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Only an admin can unlock anyone, so an admin can't lock themselves or the last unlocked admin.
func TestAdminLockLeavesAnUnlockedAdmin(t *testing.T) {
	database := dbtest.NewDatabase(t)
	first, err := database.CreateUser("first@example.com", "First", []string{"admin"})
	require.NoError(t, err)
	second, err := database.CreateUser("second@example.com", "Second", []string{"admin"})
	require.NoError(t, err)
	viewer, err := database.CreateUser("viewer@example.com", "Viewer", []string{"viewer"})
	require.NoError(t, err)

	require.ErrorIs(t, database.LockUser(first.ID, "test", &first.ID), gwdb.ErrLockOwnAccount)
	require.NoError(t, database.LockUser(viewer.ID, "test", &first.ID))
	require.NoError(t, database.LockUser(second.ID, "test", &first.ID))
	require.NoError(t, database.UnlockUser(second.ID, first.ID))

	require.NoError(t, database.LockUser(first.ID, "test", nil))
	require.ErrorIs(t, database.LockUser(second.ID, "test", &first.ID), gwdb.ErrLockLastAdmin)
	locked, err := database.IsUserLocked(second.ID)
	require.NoError(t, err)
	require.False(t, locked)
}

// Two admins locking each other at once can't both succeed.
func TestConcurrentAdminLocksLeaveAnUnlockedAdmin(t *testing.T) {
	database := dbtest.NewDatabase(t)
	first, err := database.CreateUser("first@example.com", "First", []string{"admin"})
	require.NoError(t, err)
	second, err := database.CreateUser("second@example.com", "Second", []string{"admin"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Go(func() { errs[0] = database.LockUser(second.ID, "test", &first.ID) })
	wg.Go(func() { errs[1] = database.LockUser(first.ID, "test", &second.ID) })
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, gwdb.ErrLockLastAdmin)
		}
	}
	require.Equal(t, 1, succeeded)
	admins, err := database.UnlockedAdminEmails()
	require.NoError(t, err)
	require.Len(t, admins, 1)
}
