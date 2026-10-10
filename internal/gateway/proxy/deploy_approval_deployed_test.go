package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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
