package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// A successful promote marks its approval deployed, so the approval can't promote again. Promote responses are
// streamed, not buffered, which once skipped this step and left every approval "approved".
func TestSuccessfulPromoteMarksApprovalDeployed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		want       string
	}{
		{"success", http.StatusOK, db.DeployApprovalRequestStatusDeployed},
		{"failure", http.StatusInternalServerError, db.DeployApprovalRequestStatusApproved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, database := newProxyForDeployApprovalTest(t)
			user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
			require.NoError(t, err)
			token, err := database.CreateAPIToken(strings.Repeat("d", 64), "ci", user.ID, nil, nil, nil)
			require.NoError(t, err)
			approval, err := database.CreateDeployApprovalRequest("Deploy", "test-app",
				"abc123def4567890abc123def4567890abc123de", "main", "", nil, user.ID, nil, token.ID)
			require.NoError(t, err)
			approval, err = database.ApproveDeployApprovalRequest(approval.ID, user.ID, time.Now().Add(time.Hour), "")
			require.NoError(t, err)

			bindApprovalRelease(t, database, approval.ID)

			tracker := &deployApprovalTracker{request: approval, tokenID: token.ID, app: "test-app"}
			const promotePath = "/apps/test-app/releases/RELEASE1/promote"
			promote := httptest.NewRequest(http.MethodPost, promotePath, nil)
			promote = promote.WithContext(context.WithValue(promote.Context(), deployApprovalContextKey, tracker))
			resp := &http.Response{
				StatusCode: tc.statusCode,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("OK")),
			}
			_, err = h.processProxyResponse(httptest.NewRecorder(), promote, resp, promotePath, "ci")
			require.NoError(t, err)

			reloaded, err := database.GetDeployApprovalRequest(approval.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, reloaded.Status)
		})
	}
}

// promoteThroughProxy sends a CI token's promote of RELEASE1 through ProxyToRack.
func promoteThroughProxy(h *Handler, owner *db.User, tokenID int64) *httptest.ResponseRecorder {
	return promoteWithBodyThroughProxy(h, owner, tokenID, nil)
}

func promoteWithBodyThroughProxy(h *Handler, owner *db.User, tokenID int64, body io.Reader) *httptest.ResponseRecorder {
	tokenUser := &auth.User{
		Email: owner.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci",
		Permissions: rbac.DefaultPermissionsForRole("cicd"), DBUser: owner,
	}
	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/releases/RELEASE1/promote", body)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, tokenUser))
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	return rr
}

// An approval promotes once. The claim is taken just before the promote is forwarded: a promote the rack refuses
// releases it for a retry, a promote in flight blocks a second one, and a refused promote never holds it.
func TestPromoteClaimThroughTheProxy(t *testing.T) {
	rackStatus, rackCalls := http.StatusInternalServerError, 0
	reads := &rackReads{}
	reads.addRelease(rackRelease{ID: "RELEASE1", Build: "BUILD1"})
	h, database, _, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, r *http.Request) {
		if reads.serve(w, r) {
			return // the gateway reads the release to check its images
		}
		rackCalls++
		w.WriteHeader(rackStatus)
	})
	defer cleanup()
	app := "test-app"
	require.NoError(t, database.UpsertSetting(&app, "service_image_patterns",
		map[string]string{"*": `docker\.io/org/app:{{GIT_COMMIT}}`}, nil))
	deployer := createUserWithRole(t, database, "deployer@example.com", "deployer")
	token, err := database.CreateAPIToken(strings.Repeat("e", 64), "ci", deployer.ID, nil, nil, nil)
	require.NoError(t, err)
	approval, err := database.CreateDeployApprovalRequest("Deploy", "test-app",
		"abc123def4567890abc123def4567890abc123de", "main", "", nil, deployer.ID, nil, token.ID)
	require.NoError(t, err)
	approval, err = database.ApproveDeployApprovalRequest(approval.ID, deployer.ID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	bindApprovalRelease(t, database, approval.ID)
	_, err = database.DB().Exec("UPDATE deploy_approval_requests SET approval_expires_at = $1 WHERE id = $2",
		time.Now().Add(-time.Minute), approval.ID)
	require.NoError(t, err)

	rr := promoteThroughProxy(h, deployer, token.ID)
	require.Equal(t, http.StatusForbidden, rr.Code, "the approval window has passed: %s", rr.Body.String())
	_, err = database.ExtendDeployApprovalRequestExpiry(approval.PublicID, time.Now().Add(time.Hour))
	require.NoError(t, err)

	rr = promoteThroughProxy(h, deployer, token.ID)
	require.Equal(t, http.StatusInternalServerError, rr.Code, "the refused promote didn't hold the claim")
	require.Equal(t, 1, rackCalls)

	claimed, err := database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed, "the rack refused the promote, so its claim was released")
	rr = promoteThroughProxy(h, deployer, token.ID)
	require.Equal(t, http.StatusConflict, rr.Code, "a promote is in flight: %s", rr.Body.String())
	require.Equal(t, 1, rackCalls, "a second promote isn't forwarded")
	require.NoError(t, database.ReleaseDeployApprovalPromote(approval.ID))

	rackStatus = http.StatusOK
	rr = promoteThroughProxy(h, deployer, token.ID)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	reloaded, err := database.GetDeployApprovalRequest(approval.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusDeployed, reloaded.Status)

	rr = promoteThroughProxy(h, deployer, token.ID)
	require.Equal(t, http.StatusConflict, rr.Code, "already deployed: %s", rr.Body.String())
	require.Equal(t, 2, rackCalls)
}

// A promote the rack may still be applying keeps its claim: it was sent and got no answer, or its response broke
// off after a 2xx status. One that never reached the rack, or that the rack refused, releases it.
func TestPromoteMayHaveApplied(t *testing.T) {
	sent := fmt.Errorf("%w: %w", errRackRequestFailed, context.DeadlineExceeded)
	notSent := fmt.Errorf("%w: %w", errRackRequestFailed, &net.OpError{Op: "dial", Err: errors.New("refused")})
	brokeOff := fmt.Errorf("failed to read response body: %w", io.ErrUnexpectedEOF)
	mismatch := &rackcert.FingerprintMismatchError{Expected: "aa", Actual: "bb"}
	require.True(t, promoteMayHaveApplied(http.StatusOK, nil))
	require.False(t, promoteMayHaveApplied(http.StatusInternalServerError, nil))
	require.True(t, promoteMayHaveApplied(0, sent))
	require.True(t, promoteMayHaveApplied(0, brokeOff), "the rack answered 2xx, then the body broke off")
	require.True(t, promoteMayHaveApplied(0, errors.New("an unrecognized failure")))
	require.False(t, promoteMayHaveApplied(0, notSent))
	require.False(t, promoteMayHaveApplied(0, mismatch), "the TLS handshake failed, so nothing was sent")
	require.False(t, promoteMayHaveApplied(0, errBodyTooLarge), "the gateway refused the body before sending it")
	require.False(t, promoteMayHaveApplied(0, &requestNotSentError{errors.New("failed to read request body")}))
}

// A promote the gateway gave up on before sending it (here, its body couldn't be read) releases its claim: the
// rack never saw it, so CI can promote again.
func TestAPromoteThatWasNeverSentReleasesItsClaim(t *testing.T) {
	rackCalls := 0
	reads := &rackReads{}
	reads.addRelease(rackRelease{ID: "RELEASE1", Build: "BUILD1"})
	h, database, _, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, r *http.Request) {
		if reads.serve(w, r) {
			return // the gateway reads the release to check its images
		}
		rackCalls++
	})
	defer cleanup()
	app := "test-app"
	require.NoError(t, database.UpsertSetting(&app, "service_image_patterns",
		map[string]string{"*": `docker\.io/org/app:{{GIT_COMMIT}}`}, nil))
	deployer := createUserWithRole(t, database, "deployer@example.com", "deployer")
	token, err := database.CreateAPIToken(strings.Repeat("e", 64), "ci", deployer.ID, nil, nil, nil)
	require.NoError(t, err)
	approval, err := database.CreateDeployApprovalRequest("Deploy", "test-app",
		"abc123def4567890abc123def4567890abc123de", "main", "", nil, deployer.ID, nil, token.ID)
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(approval.ID, deployer.ID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	bindApprovalRelease(t, database, approval.ID)

	rr := promoteWithBodyThroughProxy(h, deployer, token.ID, iotest.ErrReader(errors.New("the client went away")))
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Zero(t, rackCalls)
	claimed, err := database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed, "the promote never reached the rack, so its claim was released")
}
