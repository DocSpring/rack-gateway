package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// newAutoApproveFixture is a create fixture with a settings service holding the given global settings.
func newAutoApproveFixture(t *testing.T, globals map[string]interface{}) *createApprovalFixture {
	t.Helper()
	f := newCreateApprovalFixture(t)
	f.handler.settingsService = settings.NewService(f.handler.database)
	if len(globals) > 0 {
		require.NoError(t, f.handler.settingsService.SetGlobalSettings(globals, nil))
	}
	return f
}

func createAndDecode(t *testing.T, f *createApprovalFixture) DeployApprovalRequestResponse {
	t.Helper()
	resp := f.create(t, "abc123def4567890abc123def4567890abc123de")
	require.Equal(t, http.StatusCreated, resp.StatusCode, responseError(t, resp))
	var got DeployApprovalRequestResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return got
}

// systemApprovals returns the deploy_approval_request.approve entries the gateway wrote by itself.
func systemApprovals(t *testing.T, f *createApprovalFixture) []*db.AuditLog {
	t.Helper()
	logs, err := f.handler.database.GetAuditLogs(audit.SystemActor, time.Time{}, 10)
	require.NoError(t, err)
	var approvals []*db.AuditLog
	for _, entry := range logs {
		if entry.Action == "deploy_approval_request.approve" {
			approvals = append(approvals, entry)
		}
	}
	return approvals
}

// Requests wait for a person unless deploy_approval_auto_approve is on.
func TestCreateLeavesRequestsPendingByDefault(t *testing.T) {
	f := newAutoApproveFixture(t, nil)
	got := createAndDecode(t, f)
	require.Equal(t, db.DeployApprovalRequestStatusPending, got.Status)
	require.Empty(t, systemApprovals(t, f))
}

// With deploy_approval_auto_approve on, a new request is approved as it's created, with the configured window and
// no approving user, and the approval is audit-logged as the gateway's own, after the request's create entry.
func TestCreateAutoApprovesWhenEnabled(t *testing.T) {
	f := newAutoApproveFixture(t, map[string]interface{}{
		settings.KeyDeployApprovalAutoApprove:   true,
		settings.KeyDeployApprovalWindowMinutes: 30,
	})
	before := time.Now()
	got := createAndDecode(t, f)

	require.Equal(t, db.DeployApprovalRequestStatusApproved, got.Status)
	require.Empty(t, got.ApprovedByEmail)
	require.Equal(t, autoApprovalNote, got.ApprovalNotes)
	require.NotNil(t, got.ApprovalExpiresAt)
	require.WithinDuration(t, before.Add(30*time.Minute), *got.ApprovalExpiresAt, time.Minute)

	stored, err := f.handler.database.GetDeployApprovalRequestByPublicID(got.PublicID)
	require.NoError(t, err)
	require.Nil(t, stored.ApprovedByUserID)
	require.NotNil(t, stored.ApprovedAt)

	approvals := systemApprovals(t, f)
	require.Len(t, approvals, 1)
	require.Equal(t, fmt.Sprintf("%d", stored.ID), approvals[0].Resource)
	require.NotNil(t, approvals[0].DeployApprovalRequestID)
	require.Equal(t, stored.ID, *approvals[0].DeployApprovalRequestID)
	var details map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(approvals[0].Details), &details))
	require.Equal(t, true, details["auto_approved"])
	require.Equal(t, got.PublicID, details["public_id"])
	require.Equal(t, "myapp", details["app"])

	require.NotNil(t, createAuditDetails(t, f), "the request's create entry is still written")
}

// The deployment config can turn it on (RGW_SETTING_DEPLOY_APPROVAL_AUTO_APPROVE).
func TestCreateAutoApprovesWhenTheEnvironmentEnablesIt(t *testing.T) {
	t.Setenv("RGW_SETTING_DEPLOY_APPROVAL_AUTO_APPROVE", "true")
	f := newAutoApproveFixture(t, nil)
	require.Equal(t, db.DeployApprovalRequestStatusApproved, createAndDecode(t, f).Status)
}

// Auto-approval never overrides deploy_approval_require_different_approver: the request waits for an approver.
func TestCreateDoesNotAutoApproveWhenADifferentApproverIsRequired(t *testing.T) {
	f := newAutoApproveFixture(t, map[string]interface{}{
		settings.KeyDeployApprovalAutoApprove:              true,
		settings.KeyDeployApprovalRequireDifferentApprover: true,
	})
	got := createAndDecode(t, f)
	require.Equal(t, db.DeployApprovalRequestStatusPending, got.Status)
	require.Empty(t, systemApprovals(t, f))
}

// A request for a commit that already has an approved request isn't approved again: the create refuses it with
// the existing request, as without auto-approval.
func TestCreateAutoApprovalKeepsTheOneActiveRequestRule(t *testing.T) {
	f := newAutoApproveFixture(t, map[string]interface{}{settings.KeyDeployApprovalAutoApprove: true})
	first := createAndDecode(t, f)

	resp := f.create(t, "abc123def4567890abc123def4567890abc123de")
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	var conflict DeployApprovalRequestResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&conflict))
	require.Equal(t, first.PublicID, conflict.PublicID)
	require.Len(t, systemApprovals(t, f), 1)
}
