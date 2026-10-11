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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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

			tracker := &deployApprovalTracker{request: approval, tokenID: token.ID, app: "test-app"}
			withTracker := func(req *http.Request) *http.Request {
				return req.WithContext(context.WithValue(req.Context(), deployApprovalContextKey, tracker))
			}
			upload := withTracker(httptest.NewRequest(http.MethodPost, "/apps/test-app/objects/tmp/a.tgz", nil))
			require.NoError(t, h.updateObjectURLApprovalTracking(upload, "object://test-app/tmp/a.tgz"))
			// As in production: build creation records the build, build completion the release.
			require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "BUILD1", ""))
			require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "BUILD1", "RELEASE1"))

			const promotePath = "/apps/test-app/releases/RELEASE1/promote"
			promote := withTracker(httptest.NewRequest(http.MethodPost, promotePath, nil))
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
	tokenUser := &auth.User{
		Email: owner.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci",
		Permissions: rbac.DefaultPermissionsForRole("cicd"), DBUser: owner,
	}
	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/releases/RELEASE1/promote", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, tokenUser))
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	return rr
}

// An approval promotes once. The claim is taken just before the promote is forwarded: a promote the rack refuses
// releases it for a retry, a promote in flight blocks a second one, and a refused promote never holds it.
func TestPromoteClaimThroughTheProxy(t *testing.T) {
	rackStatus, rackCalls := http.StatusInternalServerError, 0
	h, database, _, cleanup := newProxyWithRackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		rackCalls++
		w.WriteHeader(rackStatus)
	})
	defer cleanup()
	deployer := createUserWithRole(t, database, "deployer@example.com", "deployer")
	token, err := database.CreateAPIToken(strings.Repeat("e", 64), "ci", deployer.ID, nil, nil, nil)
	require.NoError(t, err)
	approval, err := database.CreateDeployApprovalRequest("Deploy", "test-app",
		"abc123def4567890abc123def4567890abc123de", "main", "", nil, deployer.ID, nil, token.ID)
	require.NoError(t, err)
	approval, err = database.ApproveDeployApprovalRequest(approval.ID, deployer.ID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(approval.ID, "object://test-app/tmp/a.tgz"))
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "BUILD1", ""))
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "BUILD1", "RELEASE1"))

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

// A promote the rack may still be applying (sent, then no response) keeps its claim; one that never reached the
// rack, or that the rack refused, releases it.
func TestPromoteMayHaveApplied(t *testing.T) {
	sent := fmt.Errorf("%w: %w", errRackRequestFailed, context.DeadlineExceeded)
	notSent := fmt.Errorf("%w: %w", errRackRequestFailed, &net.OpError{Op: "dial", Err: errors.New("refused")})
	require.True(t, promoteMayHaveApplied(http.StatusOK, nil))
	require.False(t, promoteMayHaveApplied(http.StatusInternalServerError, nil))
	require.True(t, promoteMayHaveApplied(0, sent))
	require.False(t, promoteMayHaveApplied(0, notSent))
	require.False(t, promoteMayHaveApplied(0, errors.New("failed to read request body")))
}
