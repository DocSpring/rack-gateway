package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type differentApproverFixture struct {
	database *db.Database
	handler  *AdminHandler
	alice    *db.User // an admin who owns token
	bob      *db.User // another admin
	token    *db.APIToken
	commits  []string
}

func newDifferentApproverFixture(t *testing.T, required bool) *differentApproverFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	alice, err := database.CreateUser("alice@example.com", "Alice", []string{"admin"})
	require.NoError(t, err)
	bob, err := database.CreateUser("bob@example.com", "Bob", []string{"admin"})
	require.NoError(t, err)
	token, err := database.CreateAPIToken(strings.Repeat("c", 64), "alice-ci", alice.ID, []string{"deployer"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.UpsertSetting(nil, settings.KeyDeployApprovalRequireDifferentApprover, required, nil))

	return &differentApproverFixture{
		database: database,
		alice:    alice,
		bob:      bob,
		token:    token,
		handler: &AdminHandler{
			rbac:            newAllowAllRBAC(alice, bob),
			database:        database,
			auditLogger:     audit.NewLogger(database),
			config:          &config.Config{},
			settingsService: settings.NewService(database),
		},
	}
}

// request creates a pending request for alice's token, made by creator.
func (f *differentApproverFixture) request(t *testing.T, creator *db.User) *db.DeployApprovalRequest {
	t.Helper()
	commit := strings.Repeat(fmt.Sprintf("%02x", len(f.commits)), 20)
	f.commits = append(f.commits, commit)
	req, err := f.database.CreateDeployApprovalRequest(
		"Deploy", "myapp", commit, "main", "", []byte("{}"), creator.ID, nil, f.token.ID,
	)
	require.NoError(t, err)
	return req
}

func callDeployApprovalAction(
	t *testing.T,
	handle gin.HandlerFunc,
	approver *db.User,
	req *db.DeployApprovalRequest,
) (int, string) {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodPost, "/deploy-approval-requests/"+req.PublicID+"/approve", nil)
	httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), auth.UserContextKey, &auth.User{
		Email: approver.Email, Name: approver.Name, Roles: approver.Roles,
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httpReq
	c.Params = gin.Params{{Key: "id", Value: req.PublicID}}
	c.Set("user_email", approver.Email)
	handle(c)
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body.Error
}

func TestApproveOwnDeployRequestAllowedByDefault(t *testing.T) {
	f := newDifferentApproverFixture(t, false)
	status, msg := callDeployApprovalAction(t, f.handler.ApproveDeployApprovalRequest, f.alice, f.request(t, f.alice))
	require.Equal(t, http.StatusOK, status, msg)
}

func TestDifferentApproverRequired(t *testing.T) {
	f := newDifferentApproverFixture(t, true)

	status, msg := callDeployApprovalAction(t, f.handler.ApproveDeployApprovalRequest, f.alice, f.request(t, f.alice))
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "this gateway requires a different person to approve a deploy: you created this request", msg)

	// Bob asks for a deploy with alice's CI token: alice can't approve it either.
	status, msg = callDeployApprovalAction(t, f.handler.ApproveDeployApprovalRequest, f.alice, f.request(t, f.bob))
	require.Equal(t, http.StatusForbidden, status)
	require.Contains(t, msg, "you own the API token this request is for")

	req := f.request(t, f.alice)
	status, msg = callDeployApprovalAction(t, f.handler.ApproveDeployApprovalRequest, f.bob, req)
	require.Equal(t, http.StatusOK, status, msg)
	logs, err := f.database.GetAuditLogs(f.bob.Email, time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.Equal(t, "deploy_approval_request.approve", logs[0].Action)
	require.Contains(t, logs[0].Details, `"different_approver_required":"true"`)
	// The request's own audit history finds it.
	require.Equal(t, "deploy_approval_request", logs[0].ResourceType)
	require.Equal(t, &req.ID, logs[0].DeployApprovalRequestID)
}

// An approval window that can't be read (a stored value that isn't a whole number of minutes) refuses approvals and
// extensions, rather than opening the default window, which may be longer than the one configured.
func TestAnUnreadableApprovalWindowRefusesApprovals(t *testing.T) {
	f := newDifferentApproverFixture(t, false)
	require.NoError(t, f.database.UpsertSetting(nil, settings.KeyDeployApprovalWindowMinutes, "1", nil))
	req := f.request(t, f.alice)

	status, msg := callDeployApprovalAction(t, f.handler.ApproveDeployApprovalRequest, f.bob, req)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, "failed to read deploy approval settings", msg)
	status, _ = callDeployApprovalAction(t, f.handler.ExtendDeployApprovalRequest, f.bob, req)
	require.Equal(t, http.StatusInternalServerError, status)

	reloaded, err := f.database.GetDeployApprovalRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusPending, reloaded.Status)
}

func TestDifferentApproverRequiredToExtend(t *testing.T) {
	f := newDifferentApproverFixture(t, true)
	req := f.request(t, f.alice)
	_, err := f.database.ApproveDeployApprovalRequest(req.ID, f.bob.ID, time.Now().Add(-time.Minute), "", 0)
	require.NoError(t, err)

	status, msg := callDeployApprovalAction(t, f.handler.ExtendDeployApprovalRequest, f.alice, req)
	require.Equal(t, http.StatusForbidden, status)
	require.Contains(t, msg, "you created this request")

	status, msg = callDeployApprovalAction(t, f.handler.ExtendDeployApprovalRequest, f.bob, req)
	require.Equal(t, http.StatusOK, status, msg)
}
