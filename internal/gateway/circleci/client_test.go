package circleci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	rerunWorkflowID = "22222222-3333-4444-5555-666666666666"
	setupWorkflowID = "33333333-4444-5555-6666-777777777777"
	olderWorkflowID = "44444444-5555-6666-7777-888888888888"
)

// fakeWorkflow is a workflow in the fake pipeline with one approval job, approve_deploy_us, whose ID is
// "hold-<workflow id>" and whose status is hold.
type fakeWorkflow struct {
	id, name, createdAt, hold string
}

// fakeCircleCI serves the workflow, pipeline and job endpoints ApproveJob uses, and records what it approves.
type fakeCircleCI struct {
	pipeline  fakePipeline
	workflows []fakeWorkflow
	listing   []string // the workflow IDs GET /pipeline/{id}/workflow returns, newest first

	mu        sync.Mutex
	approvals []string // "<workflow id>/<job id>"
	listings  int
}

func (f *fakeCircleCI) workflow(id string) (fakeWorkflow, bool) {
	for _, wf := range f.workflows {
		if wf.id == id {
			return wf, true
		}
	}
	return fakeWorkflow{}, false
}

func (f *fakeCircleCI) approved() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.approvals...)
}

func (f *fakeCircleCI) listingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listings
}

func (f *fakeCircleCI) serveWorkflow(w http.ResponseWriter, path string) {
	id, jobs := strings.CutSuffix(strings.TrimPrefix(path, "/workflow/"), "/job")
	wf, ok := f.workflow(id)
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case jobs:
		writeJSON(w, map[string]interface{}{"items": []map[string]string{
			{"id": "hold-" + wf.id, "name": "approve_deploy_us", "type": "approval", "status": wf.hold},
		}})
	default:
		writeJSON(w, map[string]string{
			"id": wf.id, "name": wf.name, "pipeline_id": testPipelineID, "created_at": wf.createdAt,
		})
	}
}

func (f *fakeCircleCI) servePipeline(w http.ResponseWriter, path string) {
	switch path {
	case "/pipeline/" + testPipelineID:
		vcs := map[string]string{
			"revision":              f.pipeline.revision,
			"target_repository_url": f.pipeline.targetRepo,
			"origin_repository_url": f.pipeline.originRepo,
		}
		writeJSON(w, map[string]interface{}{"id": testPipelineID, "project_slug": f.pipeline.slug, "vcs": vcs})
	case "/pipeline/" + testPipelineID + "/workflow":
		f.mu.Lock()
		f.listings++
		f.mu.Unlock()
		items := make([]map[string]string, 0, len(f.listing))
		for _, id := range f.listing {
			wf, _ := f.workflow(id)
			items = append(items, map[string]string{"id": wf.id, "name": wf.name, "created_at": wf.createdAt})
		}
		writeJSON(w, map[string]interface{}{"items": items})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeCircleCI) client(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/approve/"):
			approval := strings.Replace(strings.TrimPrefix(r.URL.Path, "/workflow/"), "/approve", "", 1)
			f.mu.Lock()
			f.approvals = append(f.approvals, approval)
			f.mu.Unlock()
			writeJSON(w, map[string]string{"message": "Accepted."})
		case strings.HasPrefix(r.URL.Path, "/workflow/"):
			f.serveWorkflow(w, r.URL.Path)
		default:
			f.servePipeline(w, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient("token")
	client.BaseURL = server.URL
	return client
}

func writeJSON(w http.ResponseWriter, v interface{}) { _ = json.NewEncoder(w).Encode(v) }

func approveForTest(client *Client) error {
	return client.ApproveJob(testWorkflowID, "100", "approve_deploy_us",
		ApprovalExpectation{Revision: approvedSHA, Repo: "DocSpring/docspring"})
}

var matchingPipeline = fakePipeline{revision: approvedSHA, targetRepo: docspringRepo, originRepo: docspringRepo}

// CircleCI's pipeline workflow listing can leave out a workflow for a while (listing only setup-workflow). The
// stored workflow's hold job is approved without the listing.
func TestApproveJobApprovesStoredWorkflowMissingFromListing(t *testing.T) {
	fake := &fakeCircleCI{
		pipeline: matchingPipeline,
		workflows: []fakeWorkflow{
			{id: testWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T03:00:01Z", hold: "on_hold"},
			{id: setupWorkflowID, name: "setup-workflow", createdAt: "2026-10-13T03:00:00Z", hold: "success"},
		},
		listing: []string{setupWorkflowID},
	}
	require.NoError(t, approveForTest(fake.client(t)))
	require.Equal(t, []string{testWorkflowID + "/hold-" + testWorkflowID}, fake.approved())
	require.Zero(t, fake.listingCount(), "a hold job that's on hold is approved without listing the pipeline")
}

// A rerun is a newer workflow with the same name in the same pipeline. Its hold job is approved.
func TestApproveJobApprovesTheNewestRerun(t *testing.T) {
	fake := &fakeCircleCI{
		pipeline: matchingPipeline,
		workflows: []fakeWorkflow{
			{id: testWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T03:00:01Z", hold: "canceled"},
			{id: rerunWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T04:00:00.123Z", hold: "on_hold"},
			{id: setupWorkflowID, name: "setup-workflow", createdAt: "2026-10-13T03:00:00Z", hold: "success"},
		},
		listing: []string{rerunWorkflowID, testWorkflowID, setupWorkflowID},
	}
	require.NoError(t, approveForTest(fake.client(t)))
	require.Equal(t, []string{rerunWorkflowID + "/hold-" + rerunWorkflowID}, fake.approved())
}

// Without a rerun, the stored workflow's hold job is approved even when it isn't on hold, so CircleCI's response
// says why it can't be. An older workflow with the same name isn't a rerun.
func TestApproveJobIgnoresOlderWorkflowsWithTheSameName(t *testing.T) {
	fake := &fakeCircleCI{
		pipeline: matchingPipeline,
		workflows: []fakeWorkflow{
			{id: testWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T03:00:01Z", hold: "success"},
			{id: olderWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T02:00:00Z", hold: "on_hold"},
		},
		listing: []string{olderWorkflowID},
	}
	require.NoError(t, approveForTest(fake.client(t)))
	require.Equal(t, []string{testWorkflowID + "/hold-" + testWorkflowID}, fake.approved())
}

// A rerun's pipeline is the verified pipeline, so a mismatch stops the approval before any rerun is considered.
func TestApproveJobChecksThePipelineBeforeFollowingARerun(t *testing.T) {
	fake := &fakeCircleCI{
		pipeline: fakePipeline{revision: strings.Repeat("f", 40), targetRepo: docspringRepo, originRepo: docspringRepo},
		workflows: []fakeWorkflow{
			{id: testWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T03:00:01Z", hold: "canceled"},
			{id: rerunWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T04:00:00Z", hold: "on_hold"},
		},
		listing: []string{rerunWorkflowID, testWorkflowID},
	}
	require.ErrorIs(t, approveForTest(fake.client(t)), ErrPipelineMismatch)
	require.Empty(t, fake.approved())
}
