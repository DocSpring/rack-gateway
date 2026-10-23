package circleci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	// pageSize, when set, splits the workflow listing into pages of that many workflows, and puts a build job on
	// the first page of each workflow's job listing, before its approval job.
	pageSize int

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

func (f *fakeCircleCI) serveWorkflow(w http.ResponseWriter, path, token string) {
	id, jobs := strings.CutSuffix(strings.TrimPrefix(path, "/workflow/"), "/job")
	wf, ok := f.workflow(id)
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case jobs && f.pageSize > 0 && token == "":
		writeJSON(w, map[string]interface{}{"next_page_token": "jobs-2", "items": []map[string]string{
			{"id": "build-" + wf.id, "name": "build", "type": "build", "status": "success"},
		}})
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

func (f *fakeCircleCI) servePipeline(w http.ResponseWriter, path, token string) {
	switch path {
	case "/pipeline/" + testPipelineID:
		vcs := map[string]string{
			"revision":              f.pipeline.revision,
			"target_repository_url": f.pipeline.targetRepo,
			"origin_repository_url": f.pipeline.originRepo,
		}
		writeJSON(w, map[string]interface{}{
			"id": testPipelineID, "number": 100, "project_slug": f.pipeline.slug, "vcs": vcs,
		})
	case "/pipeline/" + testPipelineID + "/workflow":
		f.mu.Lock()
		f.listings++
		f.mu.Unlock()
		listing, next := f.listingPage(token)
		items := make([]map[string]string, 0, len(listing))
		for _, id := range listing {
			wf, _ := f.workflow(id)
			items = append(items, map[string]string{"id": wf.id, "name": wf.name, "created_at": wf.createdAt})
		}
		writeJSON(w, map[string]interface{}{"items": items, "next_page_token": next})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// listingPage returns the page of the workflow listing that token (a start index, "" for 0) starts, and the next
// page's token ("" for none).
func (f *fakeCircleCI) listingPage(token string) ([]string, string) {
	if f.pageSize == 0 {
		return f.listing, ""
	}
	start, _ := strconv.Atoi(token)
	end := min(start+f.pageSize, len(f.listing))
	if end == len(f.listing) {
		return f.listing[start:end], ""
	}
	return f.listing[start:end], strconv.Itoa(end)
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
			f.serveWorkflow(w, r.URL.Path, r.URL.Query().Get("page-token"))
		default:
			f.servePipeline(w, r.URL.Path, r.URL.Query().Get("page-token"))
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient("token")
	client.BaseURL = server.URL
	return client
}

func writeJSON(w http.ResponseWriter, v interface{}) { _ = json.NewEncoder(w).Encode(v) }

// testExpectation is the approval the fake pipeline matches: pipeline 100 building approvedSHA of
// github.com/DocSpring/docspring.
var testExpectation = ApprovalExpectation{
	Revision: approvedSHA, Repo: "github.com/DocSpring/docspring", PipelineNumber: "100",
}

func approveForTest(client *Client) error {
	return approveWith(client, testWorkflowID, testExpectation)
}

func approveWith(client *Client, workflowID string, expect ApprovalExpectation) error {
	target, err := client.PrepareApproval(workflowID, "approve_deploy_us", expect)
	if err != nil {
		return err
	}
	return client.Approve(target)
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

// The rerun and the hold job can be on later pages of CircleCI's listings.
func TestApproveJobFollowsListingPages(t *testing.T) {
	fake := &fakeCircleCI{
		pipeline: matchingPipeline,
		workflows: []fakeWorkflow{
			{id: testWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T03:00:01Z", hold: "canceled"},
			{id: rerunWorkflowID, name: "test_build_deploy", createdAt: "2026-10-13T04:00:00Z", hold: "on_hold"},
			{id: setupWorkflowID, name: "setup-workflow", createdAt: "2026-10-13T03:00:00Z", hold: "success"},
		},
		listing:  []string{setupWorkflowID, testWorkflowID, rerunWorkflowID},
		pageSize: 1,
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
