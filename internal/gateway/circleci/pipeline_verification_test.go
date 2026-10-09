package circleci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testWorkflowID = "11111111-2222-3333-4444-555555555555"
	testPipelineID = "99999999-8888-7777-6666-555555555555"
	approvedSHA    = "0123456789abcdef0123456789abcdef01234567"
	docspringRepo  = "https://github.com/DocSpring/docspring"
)

type fakePipeline struct {
	revision, targetRepo, originRepo, slug string
}

// newFakeCircleCI serves the workflow/pipeline/job endpoints ApproveJob uses and counts approvals.
func newFakeCircleCI(t *testing.T, pipeline fakePipeline) (*Client, *int32) {
	t.Helper()
	var approvals int32
	write := func(w http.ResponseWriter, v interface{}) { _ = json.NewEncoder(w).Encode(v) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/approve/"):
			atomic.AddInt32(&approvals, 1)
			write(w, map[string]string{"message": "Accepted."})
		case r.URL.Path == "/workflow/"+testWorkflowID:
			write(w, map[string]string{"id": testWorkflowID, "name": "deploy", "pipeline_id": testPipelineID})
		case r.URL.Path == "/pipeline/"+testPipelineID:
			vcs := map[string]string{
				"revision":              pipeline.revision,
				"target_repository_url": pipeline.targetRepo,
				"origin_repository_url": pipeline.originRepo,
			}
			write(w, map[string]interface{}{"id": testPipelineID, "project_slug": pipeline.slug, "vcs": vcs})
		case r.URL.Path == "/pipeline/"+testPipelineID+"/workflow":
			write(w, map[string]interface{}{"items": []map[string]string{{"id": testWorkflowID, "name": "deploy"}}})
		case r.URL.Path == "/workflow/"+testWorkflowID+"/job":
			write(w, map[string]interface{}{"items": []map[string]string{
				{"id": "job-1", "name": "approve_deploy_us", "type": "approval"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient("token")
	client.BaseURL = server.URL
	return client, &approvals
}

func TestApproveJobApprovesMatchingPipeline(t *testing.T) {
	client, approvals := newFakeCircleCI(t, fakePipeline{
		revision: approvedSHA, targetRepo: docspringRepo, originRepo: docspringRepo, slug: "gh/DocSpring/docspring",
	})
	err := client.ApproveJob(testWorkflowID, "100", "approve_deploy_us",
		ApprovalExpectation{Revision: approvedSHA, Repo: "DocSpring/docspring"})
	require.NoError(t, err)
	require.EqualValues(t, 1, *approvals)
}

func TestApproveJobRefusesMismatchedPipelines(t *testing.T) {
	cases := map[string]fakePipeline{
		"different commit": {
			revision: strings.Repeat("f", 40), targetRepo: docspringRepo, originRepo: docspringRepo,
		},
		"different repository": {
			revision: approvedSHA, targetRepo: "https://github.com/attacker/docspring",
			originRepo: "https://github.com/attacker/docspring",
		},
		"fork pipeline": {
			revision: approvedSHA, targetRepo: docspringRepo, originRepo: "https://github.com/attacker/docspring",
		},
		"different project slug": {revision: approvedSHA, slug: "gh/attacker/other"},
		// A GitHub App project slug doesn't name the repository; with no repository URL either, it
		// can't be checked, so it isn't approved.
		"unknown repository": {revision: approvedSHA, slug: "circleci/org-id/project-id"},
	}
	for name, pipeline := range cases {
		t.Run(name, func(t *testing.T) {
			client, approvals := newFakeCircleCI(t, pipeline)
			err := client.ApproveJob(testWorkflowID, "100", "approve_deploy_us",
				ApprovalExpectation{Revision: approvedSHA, Repo: "DocSpring/docspring"})
			require.ErrorIs(t, err, ErrPipelineMismatch)
			require.EqualValues(t, 0, *approvals, "nothing may be approved on a mismatch")
		})
	}
}

func TestApproveJobRejectsNonUUIDWorkflowID(t *testing.T) {
	client, approvals := newFakeCircleCI(t, fakePipeline{revision: approvedSHA})
	for _, id := range []string{"test-workflow-1", "../pipeline/x", ""} {
		err := client.ApproveJob(id, "100", "approve_deploy_us", ApprovalExpectation{Revision: approvedSHA})
		require.ErrorIs(t, err, ErrInvalidWorkflowID)
	}
	require.EqualValues(t, 0, *approvals)
}

func TestParseMetadataRequiresUUIDWorkflowID(t *testing.T) {
	_, err := ParseMetadata(map[string]interface{}{
		"workflow_id": "not-a-uuid", "pipeline_number": "1", "approval_job_name": "hold",
	})
	require.ErrorIs(t, err, ErrInvalidWorkflowID)
}

// Jobs queued before the approved revision was stored have no revision to check and are never approved.
func TestVerifyPipelineRequiresApprovedRevision(t *testing.T) {
	pipeline := &pipelineDetails{}
	pipeline.VCS.Revision = approvedSHA
	require.ErrorIs(t, verifyPipeline(pipeline, ApprovalExpectation{}), ErrPipelineMismatch)
}
