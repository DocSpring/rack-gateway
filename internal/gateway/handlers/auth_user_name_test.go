package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestFillUserNameFromIdentityProvider(t *testing.T) {
	database := dbtest.NewDatabase(t)
	h := &AuthHandler{database: database}

	seeded, err := database.CreateUser("seeded@example.com", "", []string{"admin"})
	require.NoError(t, err)
	h.fillUserName(seeded, "  Jane Doe ")
	require.Equal(t, "Jane Doe", seeded.Name)
	stored, err := database.GetUser("seeded@example.com")
	require.NoError(t, err)
	require.Equal(t, "Jane Doe", stored.Name)

	// A name set in the gateway is kept.
	named, err := database.CreateUser("named@example.com", "Chosen Name", []string{"viewer"})
	require.NoError(t, err)
	h.fillUserName(named, "Provider Name")
	stored, err = database.GetUser("named@example.com")
	require.NoError(t, err)
	require.Equal(t, "Chosen Name", stored.Name)

	// A name that doesn't fit the column is ignored.
	unnamed, err := database.CreateUser("long@example.com", "", []string{"viewer"})
	require.NoError(t, err)
	h.fillUserName(unnamed, strings.Repeat("x", maxUserNameLength+1))
	stored, err = database.GetUser("long@example.com")
	require.NoError(t, err)
	require.Empty(t, stored.Name)
}
