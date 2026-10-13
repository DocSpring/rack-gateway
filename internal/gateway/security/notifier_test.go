package security

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Alerts go to the admins at the time of the alert: one added after startup gets them, and a locked one doesn't.
func TestNotifierLooksUpAdminsWhenAlerting(t *testing.T) {
	database := dbtest.NewDatabase(t)
	n := NewNotifier(email.NoopSender{}, nil, database, nil)
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
