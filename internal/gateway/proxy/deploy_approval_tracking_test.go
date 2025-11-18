package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func newProxyForDeployApprovalTest(t *testing.T) (*Handler, *db.Database) {
	t.Helper()

	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {
			Name:     "default",
			URL:      "http://example.com",
			Username: "convox",
			APIKey:   "token",
			Enabled:  true,
		},
	}}

	h := NewHandler(
		cfg,
		mgr,
		audit.NewLogger(database),
		database,
		settings.NewService(database),
		email.NoopSender{},
		"default",
		"default",
		nil,
		nil,
		nil,
	)

	return h, database
}

// TestUpdateObjectURLApprovalTracking_WithTracker tests that object_url is saved
// when a deploy approval tracker is present in the request context
func TestUpdateObjectURLApprovalTracking_WithTracker(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)

	// Create a user and API token
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	apiToken, err := database.CreateAPIToken(
		tokenHash,
		"test-token",
		user.ID,
		[]string{"convox:build:create"},
		nil,
		nil,
	)
	require.NoError(t, err)

	// Create a deploy approval request
	approvalReq, err := database.CreateDeployApprovalRequest(
		"Test deployment", // message
		"test-app",        // app
		"abc123",          // gitCommitHash
		"main",            // gitBranch
		"",                // prURL
		[]byte("{}"),      // ciMetadata
		user.ID,           // createdByUserID
		nil,               // createdByAPITokenID
		apiToken.ID,       // targetAPITokenID
		&user.ID,          // targetUserID
	)
	require.NoError(t, err)

	// Approve the request
	expiresAt := time.Now().Add(1 * time.Hour)
	approvalReq, err = database.ApproveDeployApprovalRequest(
		approvalReq.ID, // id
		user.ID,        // approverUserID
		expiresAt,      // expiresAt
		"Approved",     // notes
	)
	require.NoError(t, err)

	// Create a tracker and set it in context
	tracker := &deployApprovalTracker{
		request: approvalReq,
		tokenID: apiToken.ID,
		app:     "test-app",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/objects/tmp/abc.tgz", nil)
	ctx := context.WithValue(req.Context(), deployApprovalContextKey, tracker)
	req = req.WithContext(ctx)

	// Call the function under test
	objectURL := "object://test-app/tmp/abc.tgz"
	err = h.updateObjectURLApprovalTracking(req, objectURL)
	require.NoError(t, err)

	// Verify object_url was saved to database
	updated, err := database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(t, objectURL, updated.ObjectURL, "object_url should be saved to deploy approval request")
	require.Empty(t, updated.BuildID, "build_id should still be empty")
	require.Empty(t, updated.ReleaseID, "release_id should still be empty")
}

// TestUpdateObjectURLApprovalTracking_WithoutTracker tests that object_url is NOT saved
// when no deploy approval tracker is in the request context
func TestUpdateObjectURLApprovalTracking_WithoutTracker(t *testing.T) {
	h, _ := newProxyForDeployApprovalTest(t)

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/objects/tmp/abc.tgz", nil)
	// NO tracker in context

	// Call the function under test - should log error but not fail
	objectURL := "object://test-app/tmp/abc.tgz"
	err := h.updateObjectURLApprovalTracking(req, objectURL)
	require.NoError(t, err, "should not return error even without tracker (logs error instead)")
}

// TestUpdateBuildApprovalTracking_WithTracker tests that build_id and release_id are saved
// when a deploy approval tracker is present in the request context.
// This test simulates the full workflow: object upload FIRST, then build creation.
func TestUpdateBuildApprovalTracking_WithTracker(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)

	// Create a user and API token
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	apiToken, err := database.CreateAPIToken(
		tokenHash,
		"test-token",
		user.ID,
		[]string{"convox:build:create"},
		nil,
		nil,
	)
	require.NoError(t, err)

	// Create a deploy approval request
	approvalReq, err := database.CreateDeployApprovalRequest(
		"Test deployment", // message
		"test-app",        // app
		"abc123",          // gitCommitHash
		"main",            // gitBranch
		"",                // prURL
		[]byte("{}"),      // ciMetadata
		user.ID,           // createdByUserID
		nil,               // createdByAPITokenID
		apiToken.ID,       // targetAPITokenID
		&user.ID,          // targetUserID
	)
	require.NoError(t, err)

	// Approve the request
	expiresAt := time.Now().Add(1 * time.Hour)
	approvalReq, err = database.ApproveDeployApprovalRequest(
		approvalReq.ID, // id
		user.ID,        // approverUserID
		expiresAt,      // expiresAt
		"Approved",     // notes
	)
	require.NoError(t, err)

	// IMPORTANT: Simulate object upload first (object_url must be set before build_id)
	err = database.UpdateDeployApprovalRequestObjectURL(approvalReq.ID, "object://test-app/tmp/source.tgz")
	require.NoError(t, err)

	// Create a tracker and set it in context
	tracker := &deployApprovalTracker{
		request: approvalReq,
		tokenID: apiToken.ID,
		app:     "test-app",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/builds", nil)
	ctx := context.WithValue(req.Context(), deployApprovalContextKey, tracker)
	req = req.WithContext(ctx)

	// Call the function under test - TWO separate calls to match real flow:
	// 1. Build creation returns buildID (releaseID empty)
	// 2. Build completion returns releaseID
	buildID := "BTEST123"
	releaseID := "RTEST456"

	// First call: save build_id only (release is empty initially)
	h.updateBuildApprovalTracking(req, buildID, "")

	// Verify build_id was saved
	updated, err := database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(t, buildID, updated.BuildID, "build_id should be saved after first call")
	require.Equal(t, "", updated.ReleaseID, "release_id should still be empty after first call")

	// Second call: save release_id (build completes)
	h.updateBuildApprovalTracking(req, buildID, releaseID)

	// Verify release_id was saved
	updated, err = database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(t, buildID, updated.BuildID, "build_id should still be set")
	require.Equal(t, releaseID, updated.ReleaseID, "release_id should be saved after second call")
}

// TestUpdateBuildApprovalTracking_WithoutTracker tests that build_id and release_id are NOT saved
// when no deploy approval tracker is in the request context
func TestUpdateBuildApprovalTracking_WithoutTracker(t *testing.T) {
	h, _ := newProxyForDeployApprovalTest(t)

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/builds", nil)
	// NO tracker in context

	// Call the function under test - should log error but not panic
	buildID := "BTEST123"
	releaseID := "RTEST456"
	h.updateBuildApprovalTracking(req, buildID, releaseID)
	// If this doesn't panic, the test passes (it should log an error)
}

// TestCaptureObjectUpload_CallsUpdateObjectURLApprovalTracking tests the integration
// between captureObjectUpload and updateObjectURLApprovalTracking
func TestCaptureObjectUpload_CallsUpdateObjectURLApprovalTracking(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)

	// Create a user and API token
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	apiToken, err := database.CreateAPIToken(
		tokenHash,
		"test-token",
		user.ID,
		[]string{"convox:build:create"},
		nil,
		nil,
	)
	require.NoError(t, err)

	// Create a deploy approval request
	approvalReq, err := database.CreateDeployApprovalRequest(
		"Test deployment", // message
		"test-app",        // app
		"abc123",          // gitCommitHash
		"main",            // gitBranch
		"",                // prURL
		[]byte("{}"),      // ciMetadata
		user.ID,           // createdByUserID
		nil,               // createdByAPITokenID
		apiToken.ID,       // targetAPITokenID
		&user.ID,          // targetUserID
	)
	require.NoError(t, err)

	// Approve the request
	expiresAt := time.Now().Add(1 * time.Hour)
	approvalReq, err = database.ApproveDeployApprovalRequest(
		approvalReq.ID, // id
		user.ID,        // approverUserID
		expiresAt,      // expiresAt
		"Approved",     // notes
	)
	require.NoError(t, err)

	// Create a tracker and set it in context
	tracker := &deployApprovalTracker{
		request: approvalReq,
		tokenID: apiToken.ID,
		app:     "test-app",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/objects/tmp/source.tgz", nil)
	ctx := context.WithValue(req.Context(), deployApprovalContextKey, tracker)
	req = req.WithContext(ctx)

	// Simulate object upload response
	objectURL := "object://test-app/tmp/abc123.tgz"
	responseObj := map[string]interface{}{
		"url": objectURL,
		"key": "tmp/source.tgz",
	}

	// Call captureObjectUpload
	h.captureObjectUpload(req, "/apps/test-app/objects/tmp/source.tgz", responseObj, "deployer@example.com")

	// Verify object_url was saved
	updated, err := database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(
		t,
		objectURL,
		updated.ObjectURL,
		"captureObjectUpload should save object_url via updateObjectURLApprovalTracking",
	)
}

// TestCaptureBuildCreation_CallsUpdateBuildApprovalTracking tests the integration
// between captureBuildCreation and updateBuildApprovalTracking
func TestCaptureBuildCreation_CallsUpdateBuildApprovalTracking(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)

	// Create a user and API token
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)

	tokenHash := strings.Repeat("a", 64)
	apiToken, err := database.CreateAPIToken(
		tokenHash,
		"test-token",
		user.ID,
		[]string{"convox:build:create"},
		nil,
		nil,
	)
	require.NoError(t, err)

	// Create a deploy approval request
	approvalReq, err := database.CreateDeployApprovalRequest(
		"Test deployment", // message
		"test-app",        // app
		"abc123",          // gitCommitHash
		"main",            // gitBranch
		"",                // prURL
		[]byte("{}"),      // ciMetadata
		user.ID,           // createdByUserID
		nil,               // createdByAPITokenID
		apiToken.ID,       // targetAPITokenID
		&user.ID,          // targetUserID
	)
	require.NoError(t, err)

	// Approve the request
	expiresAt := time.Now().Add(1 * time.Hour)
	approvalReq, err = database.ApproveDeployApprovalRequest(
		approvalReq.ID, // id
		user.ID,        // approverUserID
		expiresAt,      // expiresAt
		"Approved",     // notes
	)
	require.NoError(t, err)

	// IMPORTANT: Simulate object upload first (object_url must be set before build_id)
	err = database.UpdateDeployApprovalRequestObjectURL(approvalReq.ID, "object://test-app/tmp/source.tgz")
	require.NoError(t, err)

	// Create a tracker and set it in context
	tracker := &deployApprovalTracker{
		request: approvalReq,
		tokenID: apiToken.ID,
		app:     "test-app",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/test-app/builds", nil)
	ctx := context.WithValue(req.Context(), deployApprovalContextKey, tracker)
	req = req.WithContext(ctx)

	// Simulate build creation response (release is EMPTY initially - real Convox API behavior)
	buildID := "BTEST123"
	releaseID := "RTEST456"
	responseObj := map[string]interface{}{
		"id":      buildID,
		"release": "", // Empty initially!
		"status":  "running",
	}

	// Call captureBuildCreation - should save build_id only
	h.captureBuildCreation(req, "/apps/test-app/builds", responseObj, "deployer@example.com")

	// Verify build_id was saved but release_id is still empty
	updated, err := database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(
		t,
		buildID,
		updated.BuildID,
		"captureBuildCreation should save build_id via updateBuildApprovalTracking",
	)
	require.Equal(
		t,
		"",
		updated.ReleaseID,
		"release_id should still be empty after build creation",
	)

	// Simulate build completion - GET /builds/{id} returns release_id
	completedBuildObj := map[string]interface{}{
		"id":      buildID,
		"release": releaseID, // Now populated!
		"status":  "complete",
	}

	// Call captureBuildDetails - should save release_id
	h.captureBuildDetails(req, "/apps/test-app/builds/"+buildID, completedBuildObj, "deployer@example.com")

	// Verify release_id was saved
	updated, err = database.FindDeployApprovalRequest(db.DeployApprovalLookup{
		TokenID:       apiToken.ID,
		GitCommitHash: "abc123",
		StatusFilter:  "approved",
	})
	require.NoError(t, err)
	require.Equal(
		t,
		buildID,
		updated.BuildID,
		"build_id should still be set",
	)
	require.Equal(
		t,
		releaseID,
		updated.ReleaseID,
		"captureBuildDetails should save release_id when build completes",
	)
}
