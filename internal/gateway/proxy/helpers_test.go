package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// IDs are read at their fixed position in the rack path, so an app named after a resource ("releases", "builds",
// "processes") isn't mistaken for it.
func TestResourceIDsAreReadAtTheirPositionInTheRackPath(t *testing.T) {
	require.Equal(t, "R1", extractReleaseIDFromPath("/apps/releases/releases/R1/promote"))
	require.Equal(t, "R1", extractReleaseIDFromPath("/apps/myapp/releases/R1"))
	require.Empty(t, extractReleaseIDFromPath("/apps/releases/releases"))
	require.Empty(t, extractReleaseIDFromPath("/apps/releases/builds/B1"))
	require.Empty(t, extractReleaseIDFromPath("/apps/myapp/releases/"))

	require.Equal(t, "B1", extractBuildIDFromPath("/apps/builds/builds/B1"))
	require.Equal(t, "B1", extractBuildIDFromPath("/apps/myapp/builds/B1.tgz"), "a build export names its build")
	require.Empty(t, extractBuildIDFromPath("/apps/builds/processes"))

	require.Equal(t, "P1", extractProcessIDFromPath("/apps/processes/processes/P1/exec"))
	require.Empty(t, extractProcessIDFromPath("/apps/processes/services/web/processes"))
	require.Empty(t, extractProcessIDFromPath("/apps/myapp/services/processes/processes"))

	require.True(t, isBuildLogPath("/apps/builds/builds/B1/logs"))
	require.False(t, isBuildLogPath("/apps/builds/builds/logs"))
	require.False(t, isBuildLogPath("/apps/builds/logs/B1/logs/extra"))
	require.False(t, isBuildLogPath("/apps/myapp/builds/B1/logs/extra"))
}
