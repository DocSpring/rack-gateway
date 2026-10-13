package circleci

import (
	"strings"
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

// newFakeCircleCI serves one workflow, testWorkflowID, whose hold job is on hold and which the pipeline's workflow
// listing includes.
func newFakeCircleCI(t *testing.T, pipeline fakePipeline) (*Client, *fakeCircleCI) {
	t.Helper()
	original := fakeWorkflow{id: testWorkflowID, name: "deploy", createdAt: "2026-10-13T03:00:00Z", hold: "on_hold"}
	fake := &fakeCircleCI{pipeline: pipeline, workflows: []fakeWorkflow{original}, listing: []string{testWorkflowID}}
	return fake.client(t), fake
}

func TestApproveJobApprovesMatchingPipeline(t *testing.T) {
	client, fake := newFakeCircleCI(t, fakePipeline{
		revision: approvedSHA, targetRepo: docspringRepo, originRepo: docspringRepo, slug: "gh/DocSpring/docspring",
	})
	err := client.ApproveJob(testWorkflowID, "100", "approve_deploy_us",
		ApprovalExpectation{Revision: approvedSHA, Repo: "DocSpring/docspring"})
	require.NoError(t, err)
	require.Equal(t, []string{testWorkflowID + "/hold-" + testWorkflowID}, fake.approved())
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
			client, fake := newFakeCircleCI(t, pipeline)
			err := client.ApproveJob(testWorkflowID, "100", "approve_deploy_us",
				ApprovalExpectation{Revision: approvedSHA, Repo: "DocSpring/docspring"})
			require.ErrorIs(t, err, ErrPipelineMismatch)
			require.Empty(t, fake.approved(), "nothing may be approved on a mismatch")
		})
	}
}

func TestApproveJobRejectsNonUUIDWorkflowID(t *testing.T) {
	client, fake := newFakeCircleCI(t, fakePipeline{revision: approvedSHA})
	for _, id := range []string{"test-workflow-1", "../pipeline/x", ""} {
		err := client.ApproveJob(id, "100", "approve_deploy_us", ApprovalExpectation{Revision: approvedSHA})
		require.ErrorIs(t, err, ErrInvalidWorkflowID)
	}
	require.Empty(t, fake.approved())
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
