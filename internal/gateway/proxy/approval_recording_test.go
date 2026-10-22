package proxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// failBuildTracking makes recording a build in an approval fail, as it would with the database unavailable, until
// the returned function is called.
func failBuildTracking(t *testing.T, database *db.Database) func() {
	t.Helper()
	_, err := database.DB().Exec(`
		CREATE FUNCTION fail_build_tracking() RETURNS trigger AS $$
		BEGIN
			IF NEW.build_id IS DISTINCT FROM OLD.build_id THEN
				RAISE EXCEPTION 'database unavailable';
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_build_tracking BEFORE UPDATE ON deploy_approval_requests
			FOR EACH ROW EXECUTE FUNCTION fail_build_tracking();`)
	require.NoError(t, err)
	return func() {
		_, err := database.DB().Exec(`DROP TRIGGER fail_build_tracking ON deploy_approval_requests;
			DROP FUNCTION fail_build_tracking();`)
		require.NoError(t, err)
	}
}

// A build the rack created but the approval couldn't record isn't reported as a success: CI would carry on with a
// build its approval can't find. The client gets a 503 saying to create the build again, and the build claim is
// given up so it can.
func TestABuildTheApprovalCantRecordIsRefused(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)
	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)

	restore := failBuildTracking(t, f.database)
	rr := f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "the rack created build B1")
	require.Contains(t, rr.Body.String(), "create the build again")
	require.Empty(t, f.reload(t).BuildID)

	restore()
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "B2", f.reload(t).BuildID)
}

// A process started under an approval that can't record it is reported, not passed on as a success: the
// approval's later steps on the process (exec, terminate) find it by that record.
func TestAProcessTheApprovalCantRecordIsReported(t *testing.T) {
	h, _ := newProxyForDeployApprovalTest(t)
	tracker := &deployApprovalTracker{request: &db.DeployApprovalRequest{ID: 999999}, app: "docspring"}
	r := httptest.NewRequest(http.MethodPost, "/apps/docspring/services/web/processes", nil)

	err := h.captureProcessCreation(r, []byte(`{"id":"P1"}`), tracker)
	var notRecorded *approvalNotRecordedError
	require.True(t, errors.As(err, &notRecorded), "%v", err)
	require.Contains(t, err.Error(), "the rack started process P1")
	require.Equal(t, http.StatusServiceUnavailable, bodyErrorStatus(err, http.StatusInternalServerError))
}
