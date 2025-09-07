package proxy

import (
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/stretchr/testify/require"
)

func TestPathToResourceAction_AppRoutes(t *testing.T) {
	h := &Handler{}

	res, act := h.pathToResourceAction("/apps", "GET")
	require.Equal(t, "apps", res)
	require.Equal(t, "list", act)

	res, act = h.pathToResourceAction("/apps", "POST")
	require.Equal(t, "apps", res)
	require.Equal(t, "create", act)

	res, act = h.pathToResourceAction("/apps/myapp", "PUT")
	require.Equal(t, "apps", res)
	require.Equal(t, "update", act)

	res, act = h.pathToResourceAction("/apps/myapp/cancel", "POST")
	require.Equal(t, "apps", res)
	require.Equal(t, "update", act)

	res, act = h.pathToResourceAction("/apps/myapp", "DELETE")
	require.Equal(t, "apps", res)
	require.Equal(t, "delete", act)

	// Releases create mapping
	res, act = h.pathToResourceAction("/apps/myapp/releases", "POST")
	require.Equal(t, "releases", res)
	require.Equal(t, "create", act)

	// No dedicated env mapping; env is read via releases
}

func TestAPITokenPermission_Check(t *testing.T) {
	h := &Handler{}
	u := &auth.AuthUser{Permissions: []string{"convox:apps:create", "convox:builds:create"}, IsAPIToken: true}

	// Exact match
	allowed := h.hasAPITokenPermission(u, "apps", "create")
	require.True(t, allowed)

	// Not granted
	allowed = h.hasAPITokenPermission(u, "apps", "delete")
	require.False(t, allowed)

	// Wildcard matches
	u2 := &auth.AuthUser{Permissions: []string{"convox:apps:*"}, IsAPIToken: true}
	require.True(t, h.hasAPITokenPermission(u2, "apps", "update"))
	require.True(t, h.hasAPITokenPermission(u2, "apps", "delete"))
}
