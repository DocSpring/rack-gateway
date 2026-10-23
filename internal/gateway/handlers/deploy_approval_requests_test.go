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
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type createApprovalFixture struct {
	handler *APIHandler
	user    *db.User
	token   *db.APIToken
}

func newCreateApprovalFixture(t *testing.T) *createApprovalFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy User", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	permissions := []string{
		"convox:build:create-with-approval",
		"convox:object:create-with-approval",
		"convox:release:create-with-approval",
		"convox:release:promote-with-approval",
	}
	token, err := database.CreateAPIToken(tokenHash, "ci-token", user.ID, permissions, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, token.PublicID)

	handler := &APIHandler{
		rbac:        newAllowAllRBAC(user),
		database:    database,
		auditLogger: audit.NewLogger(database),
		config: &config.Config{Racks: map[string]config.RackConfig{
			"default": {Name: "staging", Enabled: true},
		}},
		approvalCreations: newApprovalCreationLimit(),
	}
	return &createApprovalFixture{handler: handler, user: user, token: token}
}

func (f *createApprovalFixture) create(t *testing.T, commit string) *http.Response {
	t.Helper()
	return f.createOnBranch(t, commit, "feature/deploy")
}

func (f *createApprovalFixture) createOnBranch(t *testing.T, commit, branch string) *http.Response {
	t.Helper()
	return f.createWith(t, map[string]string{"git_commit_hash": commit, "git_branch": branch})
}

// createWith sends a create request for app "myapp" on branch feature/deploy; fields override any of its values.
func (f *createApprovalFixture) createWith(t *testing.T, fields map[string]string) *http.Response {
	t.Helper()
	body := map[string]string{
		"message":             "Deploy release",
		"app":                 "myapp",
		"git_commit_hash":     "abc123def4567890abc123def4567890abc123de",
		"git_branch":          "feature/deploy",
		"target_api_token_id": f.token.PublicID,
	}
	for key, value := range fields {
		body[key] = value
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/deploy-approval-requests", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email:      f.user.Email,
		Name:       f.user.Name,
		IsAPIToken: true,
		TokenID:    &f.token.ID,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", f.user.Email)

	f.handler.CreateDeployApprovalRequest(c)

	resp := w.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestCreateDeployApprovalRequestResolvesTargetTokenByPublicID(t *testing.T) {
	f := newCreateApprovalFixture(t)
	resp := f.create(t, "abc123def4567890abc123def4567890abc123de")
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got DeployApprovalRequestResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, f.token.PublicID, got.TargetAPITokenID)
}

func TestCreateDeployApprovalRequestRequiresFullCommitSHA(t *testing.T) {
	f := newCreateApprovalFixture(t)
	for _, commit := range []string{"abc123d", "abc123def456", "%", strings.Repeat("z", 40)} {
		resp := f.create(t, commit)
		require.Equalf(t, http.StatusBadRequest, resp.StatusCode, "commit %q", commit)
	}
}

// A lapsed approval for the same commit doesn't block a new request: create runs the expiry sweep first, and the
// expiry is audit-logged.
func TestCreateDeployApprovalRequestExpiresLapsedApprovalsFirst(t *testing.T) {
	f := newCreateApprovalFixture(t)
	const commit = "abc123def4567890abc123def4567890abc123de"
	resp := f.create(t, commit)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var first DeployApprovalRequestResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&first))
	lapsed, err := f.handler.database.GetDeployApprovalRequestByPublicID(first.PublicID)
	require.NoError(t, err)
	_, err = f.handler.database.ApproveDeployApprovalRequest(lapsed.ID, f.user.ID, time.Now().Add(-time.Minute), "", 0)
	require.NoError(t, err)

	resp = f.create(t, commit)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	lapsed, err = f.handler.database.GetDeployApprovalRequest(lapsed.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusExpired, lapsed.Status)

	logs, err := f.handler.database.GetAuditLogs(audit.SystemActor, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "deploy_approval_request.expire", logs[0].Action)
	require.Equal(t, fmt.Sprintf("%d", lapsed.ID), logs[0].Resource)
}
