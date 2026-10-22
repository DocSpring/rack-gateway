package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var notRecorded *stepNotRecordedError
	require.True(t, errors.As(err, &notRecorded), "%v", err)
	require.Contains(t, err.Error(), "the rack started process P1")
	require.Equal(t, http.StatusServiceUnavailable, bodyErrorStatus(err, http.StatusInternalServerError))

	// An answer that doesn't name the process can't be recorded either.
	for _, body := range []string{`{}`, `{"id":""}`, `not json`} {
		err := h.captureProcessCreation(r, []byte(body), tracker)
		require.True(t, errors.As(err, &notRecorded), "%s: %v", body, err)
		require.Contains(t, err.Error(), retryProcess)
	}
}

// failUploadMarks makes recording that an upload finished fail, as it would with the database unavailable.
func failUploadMarks(t *testing.T, database *db.Database) {
	t.Helper()
	_, err := database.DB().Exec(`
		CREATE FUNCTION fail_upload_marks() RETURNS trigger AS $$
		BEGIN
			IF NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at THEN
				RAISE EXCEPTION 'database unavailable';
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_upload_marks BEFORE UPDATE ON rack_object_keys
			FOR EACH ROW EXECUTE FUNCTION fail_upload_marks();`)
	require.NoError(t, err)
}

// An upload the rack stored but the gateway couldn't mark finished isn't reported as a success: its key is
// write-once, and a build of an upload that doesn't look finished is refused, so the client is told to upload
// again, to a new key.
func TestAnUploadThatCantBeMarkedFinishedIsRefused(t *testing.T) {
	f := newClaimsFixture(t)
	failUploadMarks(t, f.database)

	rr := f.post(f.ci, claimsUploadPath, "archive")
	require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "the rack stored "+claimsObjectURL)
	require.Contains(t, rr.Body.String(), retryUpload)
}

// An upload the rack says it stored at another key leaves the key it was sent to unmarked, and a build of that key
// would be refused, so it isn't passed on as a success, with or without an approval.
func TestAnUploadAnsweredWithAnotherObjectsURLIsRefused(t *testing.T) {
	gateway := serveProxyAsAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		answerStoredObject(w, "object://app/tmp/other.tgz")
	})

	resp, err := http.Post(gateway+"/apps/app/objects/tmp/source.tgz", "application/octet-stream",
		strings.NewReader("archive"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, string(body))
	require.Contains(t, string(body), "another object's URL, object://app/tmp/other.tgz")
	require.Contains(t, string(body), retryUpload)
}

// A successful answer that doesn't say what the rack created can't be recorded, so it isn't passed on as a
// success. An upload's answer has to name the stored object, and a build's under an approval its ID. An error
// answer is passed on as it is.
func TestAnswersThatDontNameWhatTheRackCreatedAreRefused(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)

	f.rack.setStatus(http.StatusOK) // a 200 without a JSON body
	rr := f.post(f.ci, "/apps/test-app/objects/tmp/other.tgz", "archive")
	require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), retryUpload)

	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), retryBuild)
	require.Empty(t, f.reload(t).BuildID)

	f.rack.setStatus(http.StatusUnprocessableEntity)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code, "the rack's error is passed on: %s", rr.Body.String())

	f.rack.setStatus(0)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusOK, rr.Code, "the build claim was given up each time: %s", rr.Body.String())
	require.NotEmpty(t, f.reload(t).BuildID)
}
