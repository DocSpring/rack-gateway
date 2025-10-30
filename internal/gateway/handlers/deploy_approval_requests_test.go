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
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestCreateDeployApprovalRequestResolvesTargetTokenByPublicID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

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

	auditLogger := audit.NewLogger(database)
	handler := &APIHandler{
		rbac:        newAllowAllRBAC(user),
		database:    database,
		auditLogger: auditLogger,
		config: &config.Config{Racks: map[string]config.RackConfig{
			"default": {Name: "staging", Enabled: true},
		}},
	}

	body := map[string]string{
		"message":             "Deploy release",
		"app":                 "myapp",
		"git_commit_hash":     "abc123def456",
		"git_branch":          "main",
		"target_api_token_id": token.PublicID,
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/deploy-approval-requests", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.AuthUser{
		Email:      user.Email,
		Name:       user.Name,
		IsAPIToken: true,
		TokenID:    &token.ID,
	}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("user_email", user.Email)

	handler.CreateDeployApprovalRequest(c)

	resp := w.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got DeployApprovalRequestResponse
	err = json.NewDecoder(resp.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, token.PublicID, got.TargetAPITokenID)
}
