package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	}
	return &createApprovalFixture{handler: handler, user: user, token: token}
}

func (f *createApprovalFixture) create(t *testing.T, commit string) *http.Response {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"message":             "Deploy release",
		"app":                 "myapp",
		"git_commit_hash":     commit,
		"git_branch":          "feature/deploy",
		"target_api_token_id": f.token.PublicID,
	})
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
