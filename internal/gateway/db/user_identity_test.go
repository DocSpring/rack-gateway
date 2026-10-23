package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// A user is bound to the first account that signs in as them. Another account is refused, and an account can't be
// bound to two users. Changing the user's email unbinds them.
func TestBindUserSignInAccount(t *testing.T) {
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("dev@example.com", "Dev", []string{"viewer"})
	require.NoError(t, err)
	other, err := database.CreateUser("other@example.com", "Other", []string{"viewer"})
	require.NoError(t, err)

	require.NoError(t, database.BindUserSignInAccount(user.ID, "first"))
	require.NoError(t, database.BindUserSignInAccount(user.ID, "first"), "the bound account signs in again")
	require.ErrorIs(t, database.BindUserSignInAccount(user.ID, "second"), gwdb.ErrSignInAccountMismatch)
	require.ErrorIs(t, database.BindUserSignInAccount(user.ID, ""), gwdb.ErrSignInAccountMismatch)
	require.ErrorIs(t, database.BindUserSignInAccount(other.ID, "first"), gwdb.ErrSignInAccountMismatch,
		"one account can't sign in as two users")

	email := "renamed@example.com"
	_, err = database.UpdateUser(user.ID, gwdb.UserUpdate{Email: &email}, nil)
	require.NoError(t, err)
	require.NoError(t, database.BindUserSignInAccount(user.ID, "second"), "a new email binds a new account")

	require.ErrorIs(t, database.ResetUserSignInAccount(999999, nil), gwdb.ErrUserNotFound)
}
