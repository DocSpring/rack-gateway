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
	target, err := client.PrepareApproval(testWorkflowID, "approve_deploy_us", testExpectation)
	require.NoError(t, err)
	require.Equal(t, &ApprovalTarget{
		WorkflowID: testWorkflowID, JobID: "hold-" + testWorkflowID, PipelineID: testPipelineID, PipelineNumber: "100",
	}, target)
	require.Empty(t, fake.approved(), "preparing an approval approves nothing")

	require.NoError(t, client.Approve(target))
	require.Equal(t, []string{testWorkflowID + "/hold-" + testWorkflowID}, fake.approved())
}

// The project slug names the repository when the pipeline has no repository URL.
func TestApproveJobChecksTheProjectSlugWithoutARepositoryURL(t *testing.T) {
	client, fake := newFakeCircleCI(t, fakePipeline{revision: approvedSHA, slug: "gh/DocSpring/docspring"})
	require.NoError(t, approveWith(client, testWorkflowID, testExpectation))
	require.Len(t, fake.approved(), 1)
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
		"same owner/repo on another host": {
			revision: approvedSHA, targetRepo: "https://gitlab.com/DocSpring/docspring",
			originRepo: "https://gitlab.com/DocSpring/docspring",
		},
		"fork pipeline": {
			revision: approvedSHA, targetRepo: docspringRepo, originRepo: "https://github.com/attacker/docspring",
		},
		"different project slug":       {revision: approvedSHA, slug: "gh/attacker/other"},
		"project slug on another host": {revision: approvedSHA, slug: "bb/DocSpring/docspring"},
		// A GitHub App project slug doesn't name the repository; with no repository URL either, it
		// can't be checked, so it isn't approved.
		"unknown repository": {revision: approvedSHA, slug: "circleci/org-id/project-id"},
	}
	for name, pipeline := range cases {
		t.Run(name, func(t *testing.T) {
			client, fake := newFakeCircleCI(t, pipeline)
			require.ErrorIs(t, approveWith(client, testWorkflowID, testExpectation), ErrPipelineMismatch)
			require.Empty(t, fake.approved(), "nothing may be approved on a mismatch")
		})
	}
}

// The approver was shown the pipeline number the requester gave; a workflow in another pipeline of the same commit
// and repository isn't approved.
func TestApproveJobRefusesAWorkflowFromAnotherPipeline(t *testing.T) {
	client, fake := newFakeCircleCI(t, matchingPipeline)
	expect := testExpectation
	expect.PipelineNumber = "99"
	require.ErrorIs(t, approveWith(client, testWorkflowID, expect), ErrPipelineMismatch)
	expect.PipelineNumber = ""
	require.ErrorIs(t, approveWith(client, testWorkflowID, expect), ErrPipelineMismatch)
	require.Empty(t, fake.approved())
}

// Without the app's repository, the commit alone doesn't tie the pipeline to the app, so nothing is approved.
func TestApproveJobRequiresTheAppRepository(t *testing.T) {
	client, fake := newFakeCircleCI(t, matchingPipeline)
	expect := testExpectation
	expect.Repo = ""
	require.ErrorIs(t, approveWith(client, testWorkflowID, expect), ErrPipelineMismatch)
	require.Empty(t, fake.approved())
}

func TestApproveJobRejectsNonUUIDWorkflowID(t *testing.T) {
	client, fake := newFakeCircleCI(t, fakePipeline{revision: approvedSHA})
	for _, id := range []string{"test-workflow-1", "../pipeline/x", ""} {
		require.ErrorIs(t, approveWith(client, id, testExpectation), ErrInvalidWorkflowID)
	}
	require.Empty(t, fake.approved())
}

func TestRepoIdentity(t *testing.T) {
	identity, err := RepoIdentity("github", "DocSpring/docspring")
	require.NoError(t, err)
	require.Equal(t, "github.com/DocSpring/docspring", identity)
	identity, err = RepoIdentity("bitbucket", "team/app")
	require.NoError(t, err)
	require.Equal(t, "bitbucket.org/team/app", identity)

	bad := [][2]string{{"gitlab", "a/b"}, {"github", ""}, {"github", "a"}, {"github", "a/b/c"}, {"github", "/b"}}
	for _, bad := range bad {
		_, err := RepoIdentity(bad[0], bad[1])
		require.Error(t, err, "%q %q", bad[0], bad[1])
	}
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
