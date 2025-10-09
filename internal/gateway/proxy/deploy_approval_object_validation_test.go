package proxy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

// createTarGz creates a .tar.gz archive with a convox.yml file
func createTarGz(t *testing.T, convoxYml string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add convox.yml to tar
	header := &tar.Header{
		Name: "convox.yml",
		Mode: 0644,
		Size: int64(len(convoxYml)),
	}
	require.NoError(t, tw.WriteHeader(header))
	_, err := tw.Write([]byte(convoxYml))
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())

	return buf.Bytes()
}

func TestValidateObjectUpload_ValidImageTags(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create test user and API token
	user, err := database.CreateUser("ci@example.com", "CI User", []string{"cicd"})
	require.NoError(t, err)

	tokenHash := "test-token-hash"
	token, err := database.CreateAPIToken(tokenHash, "CI Token", user.ID, []string{"convox:deploy:deploy_with_approval"}, nil, nil)
	require.NoError(t, err)

	// Create approver user
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	// Create deploy approval request with git commit
	gitCommit := "abc123def456"
	req, err := database.CreateDeployApprovalRequest(
		"Test deployment",
		gitCommit,
		"main",
		"https://example.com/pipeline",
		"",
		nil,
		approver.ID,
		&token.ID,
		token.ID,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, req)

	// Approve the request
	expiresAt := time.Now().Add(15 * time.Minute)
	_, err = database.ApproveDeployApprovalRequest(req.ID, approver.ID, expiresAt, "Approved for testing")
	require.NoError(t, err)

	// Configure app to require image tag pattern validation
	patterns := map[string]string{
		"myapp": "example.com/myapp:{{GIT_COMMIT}}-amd64",
	}
	err = database.UpsertAppImageTagPatterns(patterns, &approver.ID)
	require.NoError(t, err)

	// Create convox.yml with matching image tags
	convoxYml := `services:
  web:
    image: example.com/myapp:abc123def456-amd64
    port: 3000
  worker:
    image: example.com/myapp:abc123def456-amd64
    command: bundle exec sidekiq
`

	tarGz := createTarGz(t, convoxYml)

	handler := &Handler{
		database: database,
	}

	// Create request with app path
	httpReq := httptest.NewRequest(http.MethodPost, "/apps/myapp/objects/tmp/test.tgz", nil)

	// This should pass validation
	err = handler.validateObjectUpload(httpReq, tarGz, token.ID)
	require.NoError(t, err)
}

func TestValidateObjectUpload_InvalidImageTags(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create test user and API token
	user, err := database.CreateUser("ci@example.com", "CI User", []string{"cicd"})
	require.NoError(t, err)

	tokenHash := "test-token-hash"
	token, err := database.CreateAPIToken(tokenHash, "CI Token", user.ID, []string{"convox:deploy:deploy_with_approval"}, nil, nil)
	require.NoError(t, err)

	// Create approver user
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	// Create deploy approval request with git commit
	gitCommit := "abc123def456"
	req, err := database.CreateDeployApprovalRequest(
		"Test deployment",
		gitCommit,
		"main",
		"https://example.com/pipeline",
		"",
		nil,
		approver.ID,
		&token.ID,
		token.ID,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, req)

	// Approve the request
	expiresAt := time.Now().Add(15 * time.Minute)
	_, err = database.ApproveDeployApprovalRequest(req.ID, approver.ID, expiresAt, "Approved for testing")
	require.NoError(t, err)

	// Configure app to require image tag pattern validation
	patterns := map[string]string{
		"myapp": "example.com/myapp:{{GIT_COMMIT}}-amd64",
	}
	err = database.UpsertAppImageTagPatterns(patterns, &approver.ID)
	require.NoError(t, err)

	// Create convox.yml with MISMATCHED image tags (different commit)
	convoxYml := `services:
  web:
    image: example.com/myapp:malicious999-amd64
    port: 3000
  worker:
    image: example.com/myapp:abc123def456-amd64
`

	tarGz := createTarGz(t, convoxYml)

	handler := &Handler{
		database: database,
	}

	// Create request with app path
	httpReq := httptest.NewRequest(http.MethodPost, "/apps/myapp/objects/tmp/test.tgz", nil)

	// This should fail validation because web service has wrong commit hash
	err = handler.validateObjectUpload(httpReq, tarGz, token.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manifest validation failed")
}

func TestValidateObjectUpload_NoApproval(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create test user and API token
	user, err := database.CreateUser("ci@example.com", "CI User", []string{"cicd"})
	require.NoError(t, err)

	tokenHash := "test-token-hash"
	token, err := database.CreateAPIToken(tokenHash, "CI Token", user.ID, []string{"convox:deploy:deploy_with_approval"}, nil, nil)
	require.NoError(t, err)

	// Create approver user
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	// Configure app to require image tag pattern validation
	patterns := map[string]string{
		"myapp": "example.com/myapp:{{GIT_COMMIT}}-amd64",
	}
	err = database.UpsertAppImageTagPatterns(patterns, &approver.ID)
	require.NoError(t, err)

	// Create convox.yml
	convoxYml := `services:
  web:
    image: example.com/myapp:abc123def456-amd64
    port: 3000
  worker:
    image: example.com/myapp:abc123def456-amd64
`

	tarGz := createTarGz(t, convoxYml)

	handler := &Handler{
		database: database,
	}

	// Create request with app path
	httpReq := httptest.NewRequest(http.MethodPost, "/apps/myapp/objects/tmp/test.tgz", nil)

	// This should fail because there's no active approval
	err = handler.validateObjectUpload(httpReq, tarGz, token.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deployment approval required")
}

func TestValidateObjectUpload_SkipWhenNoPatternConfigured(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	// Create test user and API token
	user, err := database.CreateUser("ci@example.com", "CI User", []string{"cicd"})
	require.NoError(t, err)

	tokenHash := "test-token-hash"
	token, err := database.CreateAPIToken(tokenHash, "CI Token", user.ID, []string{"convox:deploy:deploy_with_approval"}, nil, nil)
	require.NoError(t, err)

	// Don't configure any image tag pattern for the app

	// Create convox.yml with any image tags (should be ignored)
	convoxYml := `services:
  web:
    image: example.com/myapp:latest
    port: 3000
`

	tarGz := createTarGz(t, convoxYml)

	handler := &Handler{
		database: database,
	}

	// Create request with app path
	httpReq := httptest.NewRequest(http.MethodPost, "/apps/myapp/objects/tmp/test.tgz", nil)

	// This should pass because no pattern is configured (validation skipped)
	err = handler.validateObjectUpload(httpReq, tarGz, token.ID)
	require.NoError(t, err)
}
