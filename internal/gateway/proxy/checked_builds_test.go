package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// A build whose manifest the gateway checked with the app's env is recorded before it's forwarded, with the ID the
// rack gives it. One the rack refused never started, and is forgotten; one that may have started (here, the rack
// answered 500) is kept without an ID.
func TestCheckedBuildsThroughTheProxy(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)
	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)
	unfinished := func() []string {
		builds, err := f.database.UnfinishedCheckedBuilds(claimsApp)
		require.NoError(t, err)
		ids := make([]string, 0, len(builds))
		for _, build := range builds {
			ids = append(ids, build.BuildID)
		}
		return ids
	}

	f.rack.setStatus(http.StatusInternalServerError)
	rr := f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
	require.Equal(t, []string{""}, unfinished())

	f.rack.setStatus(http.StatusBadRequest)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Equal(t, []string{""}, unfinished())

	f.rack.setStatus(0)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, []string{"", "B3"}, unfinished())
}

// An env change waits until every build the gateway checked with the app's env has finished: the rack's build
// reads the env when it starts, after the check, and a changed env could have it run images the check never saw.
func TestEnvChangeWaitsForCheckedBuilds(t *testing.T) {
	f := newProtectedEnvFixture(t)
	id, err := f.database.RecordCheckedBuild("app")
	require.NoError(t, err)
	change := func() *httptest.ResponseRecorder {
		req, _ := f.request(t, f.deployer, "", "", "", stepUpAgeFresh)
		return f.proxy(req)
	}

	rr := change()
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "hasn't reported its ID")

	require.NoError(t, f.database.SetCheckedBuildID(id, "B1"))
	f.rack.setBuildStatus("B1", "running")
	rr = change()
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "build B1 is running")
	require.Empty(t, f.rack.releases())

	f.rack.setBuildStatus("B1", "failed")
	rr = change()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Len(t, f.rack.releases(), 1)
	builds, err := f.database.UnfinishedCheckedBuilds("app")
	require.NoError(t, err)
	require.Empty(t, builds)
}
