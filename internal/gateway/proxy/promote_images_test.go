package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const (
	promoteApp     = "test-app"
	promoteCommit  = "0123456789abcdef0123456789abcdef01234567"
	promotePattern = `docker\.io/org/app:{{GIT_COMMIT}}`
	promotePath    = "/apps/test-app/releases/RELEASE1/promote"
)

// promoteFixture promotes release RELEASE1 (built by BUILD1 from promoteCommit) through the proxy, with a token
// that holds the promote permission itself, so no deploy approval is involved.
type promoteFixture struct {
	h        *Handler
	database *db.Database
	reads    *rackReads
	promotes atomic.Int32
	token    *auth.User
}

func newPromoteFixture(t *testing.T) *promoteFixture {
	t.Helper()
	f := &promoteFixture{reads: &rackReads{}}
	h, database, _, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, r *http.Request) {
		if f.reads.serve(w, r) {
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/promote") {
			f.promotes.Add(1)
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		http.NotFound(w, r)
	})
	t.Cleanup(cleanup)
	f.h, f.database = h, database

	owner := createUserWithRole(t, database, "deployer@example.com", "deployer")
	perms := []string{rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote)}
	token, err := database.CreateAPIToken(strings.Repeat("p", 64), "deploy", owner.ID, perms, nil, nil)
	require.NoError(t, err)
	f.token = &auth.User{
		Email: owner.Email, IsAPIToken: true, TokenID: &token.ID, TokenName: "deploy", Permissions: perms,
		DBUser: owner,
	}
	f.reads.addBuild(rackBuild{ID: "BUILD1", GitSha: promoteCommit})
	return f
}

func (f *promoteFixture) setPatterns(t *testing.T) {
	t.Helper()
	app := promoteApp
	require.NoError(t, f.database.UpsertSetting(&app, "service_image_patterns",
		map[string]string{"*": promotePattern}, nil))
}

// setRelease makes RELEASE1 hold manifest and env.
func (f *promoteFixture) setRelease(manifest, env string) {
	f.reads.addRelease(rackRelease{ID: "RELEASE1", Build: "BUILD1", Manifest: manifest, Env: env})
}

func (f *promoteFixture) promote() *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, promotePath, nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, f.token))
	rr := httptest.NewRecorder()
	f.h.ProxyToRack(rr, req)
	return rr
}

func initContainerManifest(initImage string) string {
	return "services:\n  web:\n    image: docker.io/org/app:" + promoteCommit +
		"\n    initContainer:\n      image: " + initImage + "\n"
}

// A promote runs the init container image the release's manifest names once the release's env is substituted into
// it, which can differ from what the build was checked with.
func TestPromoteChecksInitContainerImagesWithTheReleaseEnv(t *testing.T) {
	f := newPromoteFixture(t)
	f.setPatterns(t)
	manifest := initContainerManifest("docker.io/org/app:${INIT_TAG}")

	f.setRelease(manifest, "INIT_TAG=attacker")
	rr := f.promote()
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `promote of release RELEASE1 refused: service web init container image `+
		`\"docker.io/org/app:attacker\" does not match required pattern`)
	require.Zero(t, f.promotes.Load(), "a refused promote isn't forwarded")

	f.setRelease(manifest, "INIT_TAG="+promoteCommit)
	rr = f.promote()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, int32(1), f.promotes.Load())
}

// Each service runs the image its build pushed, so a promote doesn't recheck the service's own image: a release
// built before the patterns changed still promotes.
func TestPromoteDoesNotRecheckServiceImages(t *testing.T) {
	f := newPromoteFixture(t)
	f.setPatterns(t)
	f.setRelease("services:\n  web:\n    image: docker.io/org/app:older\n", "")
	rr := f.promote()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}

func TestPromoteRefusesAResourceImage(t *testing.T) {
	f := newPromoteFixture(t)
	f.setPatterns(t)
	f.setRelease("resources:\n  db:\n    type: postgres\n    image: attacker/postgres\n"+
		"services:\n  web:\n    image: docker.io/org/app:"+promoteCommit+"\n", "")
	rr := f.promote()
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `resource db overrides its image (\"attacker/postgres\")`)
	require.Zero(t, f.promotes.Load())
}

// Without image patterns or a deploy approval there's nothing to check, so the release isn't read.
func TestPromoteWithoutPatternsIsNotChecked(t *testing.T) {
	f := newPromoteFixture(t)
	rr := f.promote()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Equal(t, int32(1), f.promotes.Load())
}

// A release the gateway can't read can't be checked, so the promote isn't forwarded.
func TestPromoteFailsClosedWhenTheReleaseCantBeRead(t *testing.T) {
	f := newPromoteFixture(t)
	f.setPatterns(t)
	rr := f.promote()
	require.Equal(t, http.StatusBadGateway, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "failed to check release RELEASE1's images")
	require.Zero(t, f.promotes.Load())
}

// An approval-bound promote's init container images must be tagged with the approved commit, and a refused promote
// doesn't hold the approval's promote claim.
func TestApprovalPromoteRequiresTheApprovedCommit(t *testing.T) {
	f := newPromoteFixture(t)
	f.setPatterns(t)
	owner := f.token.DBUser
	ci, err := f.database.CreateAPIToken(strings.Repeat("q", 64), "ci", owner.ID, nil, nil, nil)
	require.NoError(t, err)
	approval, err := f.database.CreateDeployApprovalRequest("Deploy", promoteApp, promoteCommit, "main", "", nil,
		owner.ID, nil, ci.ID)
	require.NoError(t, err)
	_, err = f.database.ApproveDeployApprovalRequest(approval.ID, owner.ID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	bindApprovalRelease(t, f.database, approval.ID)

	other := strings.Repeat("f", 40)
	f.setRelease(initContainerManifest("docker.io/org/app:"+other), "")
	rr := promoteThroughProxy(f.h, owner, ci.ID)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "is not tagged with the approved commit "+promoteCommit)
	require.Zero(t, f.promotes.Load())

	f.setRelease(initContainerManifest("docker.io/org/app:"+promoteCommit), "")
	rr = promoteThroughProxy(f.h, owner, ci.ID)
	require.Equal(t, http.StatusOK, rr.Code, "the refused promote didn't hold the claim: %s", rr.Body.String())
	require.Equal(t, int32(1), f.promotes.Load())
}
