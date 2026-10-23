package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const (
	claimsApp        = "test-app"
	claimsObjectURL  = "object://test-app/tmp/a.tgz"
	claimsUploadPath = "/apps/test-app/objects/tmp/a.tgz"
	claimsBuildPath  = "/apps/test-app/builds"
	claimsImportPath = "/apps/test-app/builds/import"
)

// bindApprovalArchive records an approval's archive as the proxy does: the upload claims the object before it's
// forwarded, and its response confirms it.
func bindApprovalArchive(t *testing.T, database *db.Database, id int64, objectURL string) {
	t.Helper()
	app, key, ok := strings.Cut(strings.TrimPrefix(objectURL, "object://"), "/")
	require.True(t, ok, objectURL)
	claimed, err := database.ClaimObjectUpload(app, key)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = database.ClaimDeployApprovalObjectUpload(id, objectURL)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, database.MarkObjectUploaded(app, key))
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(id, objectURL))
}

// bindApprovalRelease takes an approval through its upload of object://test-app/tmp/a.tgz and its one build, BUILD1,
// to the build's release, RELEASE1.
func bindApprovalRelease(t *testing.T, database *db.Database, id int64) {
	t.Helper()
	const buildID, releaseID = "BUILD1", "RELEASE1"
	bindApprovalArchive(t, database, id, "object://test-app/tmp/a.tgz")
	claimed, err := database.ClaimDeployApprovalBuild(id)
	require.NoError(t, err)
	require.True(t, claimed)
	// As in production: build creation records the build, build completion the release.
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(id, buildID, ""))
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(id, buildID, releaseID))
}

// claimRack is a fake rack that counts the uploads and builds it receives. A test can hold one of them until it
// lets it finish, to send a second request while the first is in flight.
type claimRack struct {
	mu      sync.Mutex
	writes  int
	status  int // status of write responses; 0 answers as the rack does on success
	hold    bool
	held    chan struct{}
	release chan struct{}
	once    sync.Once
	archive []byte // served to the gateway's manifest check
	reads   rackReads
}

func newClaimRack() *claimRack {
	return &claimRack{held: make(chan struct{}), release: make(chan struct{})}
}

func (rk *claimRack) handler(w http.ResponseWriter, r *http.Request) {
	if rk.reads.serve(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(rk.archive)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	rk.mu.Lock()
	rk.writes++
	n, hold, status := rk.writes, rk.hold, rk.status
	rk.hold = false
	rk.mu.Unlock()
	if hold {
		close(rk.held)
		<-rk.release
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(r.URL.Path, "/objects/") {
		objectURL := "object://" + claimsApp + "/tmp/" + path.Base(r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]string{"Url": objectURL})
		return
	}
	_, _ = fmt.Fprintf(w, `{"id":"B%d","release":"","status":"running"}`, n)
}

// holdNextWrite holds the next upload or build until unblock is called.
func (rk *claimRack) holdNextWrite() {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	rk.hold = true
}

func (rk *claimRack) waitForHeldWrite(t *testing.T) {
	t.Helper()
	select {
	case <-rk.held:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the held request never reached the rack")
	}
}

func (rk *claimRack) unblock() { rk.once.Do(func() { close(rk.release) }) }

func (rk *claimRack) setStatus(status int) {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	rk.status = status
}

func (rk *claimRack) writeCount() int {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	return rk.writes
}

type claimsFixture struct {
	h        *Handler
	database *db.Database
	rack     *claimRack
	deployer *auth.User
	admin    *auth.User
	ci       *auth.User // a deployer's CI token, with the cicd role's permissions
	approval *db.DeployApprovalRequest
}

func newClaimsFixture(t *testing.T) *claimsFixture {
	t.Helper()
	rack := newClaimRack()
	h, database, _, cleanup := newProxyWithRackServer(t, rack.handler)
	t.Cleanup(cleanup)
	// Cleanups run last-in first-out: a test that fails while a request is held must not leave the rack blocked.
	t.Cleanup(rack.unblock)
	f := &claimsFixture{h: h, database: database, rack: rack}

	deployer := createUserWithRole(t, database, "deployer@example.com", "deployer")
	admin := createUserWithRole(t, database, "admin@example.com", "admin")
	f.deployer = &auth.User{Email: deployer.Email, Name: deployer.Name, Roles: deployer.Roles, DBUser: deployer}
	f.admin = &auth.User{Email: admin.Email, Name: admin.Name, Roles: admin.Roles, DBUser: admin}
	token, err := database.CreateAPIToken(strings.Repeat("c", 64), "ci", deployer.ID, nil, nil, nil)
	require.NoError(t, err)
	f.ci = &auth.User{
		Email: deployer.Email, IsAPIToken: true, TokenID: &token.ID, TokenName: "ci",
		Permissions: rbac.DefaultPermissionsForRole("cicd"), DBUser: deployer,
	}

	approval, err := database.CreateDeployApprovalRequest(
		"Deploy", claimsApp, approvedCommit, "main", "", nil, deployer.ID, nil, token.ID,
	)
	require.NoError(t, err)
	f.approval, err = database.ApproveDeployApprovalRequest(approval.ID, deployer.ID, time.Now().Add(time.Hour), "", 0)
	require.NoError(t, err)
	return f
}

// allowApprovedImages lets the approval's build pass the manifest check: the archive's one service uses an image
// tagged with the approved commit.
func (f *claimsFixture) allowApprovedImages(t *testing.T) {
	t.Helper()
	app := claimsApp
	patterns := map[string]string{"*": `docker\.io/docspringcom/app:{{GIT_COMMIT}}`}
	require.NoError(t, f.database.UpsertSetting(&app, "service_image_patterns", patterns, nil))
	f.rack.archive = tarballWithManifest(t, imageManifest("docker.io/docspringcom/app:"+approvedCommit))
}

func (f *claimsFixture) request(user *auth.User, method, pth string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, pth, body)
	if pth == claimsBuildPath {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rr := httptest.NewRecorder()
	f.h.ProxyToRack(rr, req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user)))
	return rr
}

func (f *claimsFixture) post(user *auth.User, pth, body string) *httptest.ResponseRecorder {
	return f.request(user, http.MethodPost, pth, strings.NewReader(body))
}

// postHeld sends a request the rack holds, and returns once the rack has it.
func (f *claimsFixture) postHeld(t *testing.T, user *auth.User, pth, body string) <-chan *httptest.ResponseRecorder {
	t.Helper()
	f.rack.holdNextWrite()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.post(user, pth, body) }()
	f.rack.waitForHeldWrite(t)
	return done
}

func (f *claimsFixture) reload(t *testing.T) *db.DeployApprovalRequest {
	t.Helper()
	reloaded, err := f.database.GetDeployApprovalRequest(f.approval.ID)
	require.NoError(t, err)
	return reloaded
}

// An approval uploads one archive. A second upload while the first is in flight, to its key or another, isn't
// forwarded: the rack would overwrite the archive, or the approval would bind whichever upload finished last.
func TestSecondUploadWhileTheFirstIsInFlightIsRefused(t *testing.T) {
	f := newClaimsFixture(t)
	first := f.postHeld(t, f.ci, claimsUploadPath, "archive")

	for _, pth := range []string{claimsUploadPath, "/apps/test-app/objects/tmp/b.tgz"} {
		rr := f.post(f.ci, pth, "another archive")
		require.Equal(t, http.StatusConflict, rr.Code, "%s: %s", pth, rr.Body.String())
	}
	require.Equal(t, 1, f.rack.writeCount(), "a second upload isn't forwarded")

	f.rack.unblock()
	rr := <-first
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, claimsObjectURL, f.reload(t).ObjectURL)
	claimed, err := f.database.ClaimDeployApprovalBuild(f.approval.ID)
	require.NoError(t, err)
	require.True(t, claimed, "the finished upload was recorded, so the approval can build it")
}

// An object holding an approval's archive can't be uploaded again by anyone, admins included: the rack would
// replace the archive the build's manifest is checked against.
func TestUploadOverAnApprovalsArchiveIsRefusedForEveryone(t *testing.T) {
	f := newClaimsFixture(t)
	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)

	for _, user := range []*auth.User{f.deployer, f.admin} {
		rr := f.post(user, claimsUploadPath, "replacement archive")
		require.Equal(t, http.StatusConflict, rr.Code, "%s: %s", user.Email, rr.Body.String())
	}
	require.Zero(t, f.rack.writeCount(), "the rack never sees an upload over the approved archive")

	rr := f.post(f.deployer, "/apps/test-app/objects/tmp/other.tgz", "archive")
	require.Equal(t, http.StatusOK, rr.Code, "other objects upload as before: %s", rr.Body.String())
}

// An upload the rack refuses releases the approval's claim, so CI can retry it. The retry uploads to a new key, as
// the Convox CLI does: the failed upload's key stays claimed, since the rack may have stored it anyway.
func TestFailedUploadReleasesItsClaim(t *testing.T) {
	f := newClaimsFixture(t)
	f.rack.setStatus(http.StatusInternalServerError)
	rr := f.post(f.ci, claimsUploadPath, "archive")
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
	require.Empty(t, f.reload(t).ObjectURL)

	f.rack.setStatus(0)
	rr = f.post(f.ci, claimsUploadPath, "archive")
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	rr = f.post(f.ci, "/apps/test-app/objects/tmp/retry.tgz", "archive")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "object://test-app/tmp/retry.tgz", f.reload(t).ObjectURL)
}

// Object keys are write-once through the gateway: an archive can't be replaced after it's uploaded, or after a
// build's manifest was checked against it.
func TestObjectKeysAreWriteOnce(t *testing.T) {
	f := newClaimsFixture(t)
	rr := f.post(f.deployer, "/apps/test-app/objects/tmp/mine.tgz", "archive")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	for _, user := range []*auth.User{f.deployer, f.admin} {
		rr = f.post(user, "/apps/test-app/objects/tmp/mine.tgz", "replacement")
		require.Equal(t, http.StatusConflict, rr.Code, "%s: %s", user.Email, rr.Body.String())
	}
	require.Equal(t, 1, f.rack.writeCount())

	require.NoError(t, f.database.ClaimObjectForBuild("test-app", "tmp/checked.tgz"))
	rr = f.post(f.admin, "/apps/test-app/objects/tmp/checked.tgz", "replacement")
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
}

// A build can't use an archive whose upload is still in flight: the gateway would check what the rack holds now,
// and the rack build what the upload leaves.
func TestBuildOfAnArchiveStillUploadingIsRefused(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)
	upload := f.postHeld(t, f.deployer, "/apps/test-app/objects/tmp/slow.tgz", "archive")

	rr := f.post(f.deployer, claimsBuildPath, "url=object://test-app/tmp/slow.tgz&git-sha="+approvedCommit)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "upload hasn't finished")

	f.rack.unblock()
	require.Equal(t, http.StatusOK, (<-upload).Code)
	rr = f.post(f.deployer, claimsBuildPath, "url=object://test-app/tmp/slow.tgz&git-sha="+approvedCommit)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}

// An approval creates one build. A second build while the first is in flight isn't forwarded, and once the first
// is recorded the approval can't build again.
func TestSecondBuildWhileTheFirstIsInFlightIsRefused(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)
	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)
	first := f.postHeld(t, f.ci, claimsBuildPath, "url="+claimsObjectURL)

	rr := f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	require.Equal(t, 1, f.rack.writeCount(), "a second build isn't forwarded")

	f.rack.unblock()
	rr = <-first
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "B1", f.reload(t).BuildID)

	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	require.Equal(t, 1, f.rack.writeCount())
}

// A build the rack refuses releases its claim, so CI can retry it.
func TestFailedBuildReleasesItsClaim(t *testing.T) {
	f := newClaimsFixture(t)
	f.allowApprovedImages(t)
	bindApprovalArchive(t, f.database, f.approval.ID, claimsObjectURL)

	f.rack.setStatus(http.StatusInternalServerError)
	rr := f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())

	f.rack.setStatus(0)
	rr = f.post(f.ci, claimsBuildPath, "url="+claimsObjectURL)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, "B2", f.reload(t).BuildID)
}

// unreadBody is a request body that records whether anything read it.
type unreadBody struct{ read bool }

func (b *unreadBody) Read([]byte) (int, error) {
	b.read = true
	return 0, io.EOF
}

// An import brings its own manifest and images, which the gateway doesn't check, so it needs build:import: only
// admins hold it, and an approval never covers it. A refused import is never read.
func TestBuildImportIsAdminOnly(t *testing.T) {
	f := newClaimsFixture(t)
	for _, user := range []*auth.User{f.deployer, f.ci} {
		body := &unreadBody{}
		rr := f.request(user, http.MethodPost, claimsImportPath, body)
		require.Equal(t, http.StatusForbidden, rr.Code, "%s (token=%v): %s", user.Email, user.IsAPIToken,
			rr.Body.String())
		require.False(t, body.read, "the gateway must not read a refused import")
	}
	require.Zero(t, f.rack.writeCount())

	rr := f.post(f.admin, claimsImportPath, "exported build")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, 1, f.rack.writeCount())
}
