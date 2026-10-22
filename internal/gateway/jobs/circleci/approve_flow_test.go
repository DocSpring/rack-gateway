package circleci

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

const (
	flowWorkflowID = "11111111-2222-3333-4444-555555555555"
	flowPipelineID = "99999999-8888-7777-6666-555555555555"
	flowSHA        = "0123456789abcdef0123456789abcdef01234567"
)

// fakeAuditLog keeps the entries the worker writes, and can refuse attempts.
type fakeAuditLog struct {
	mu          sync.Mutex
	entries     []*db.AuditLog
	failAttempt bool
}

func (f *fakeAuditLog) RecordSystemAttempt(entry *db.AuditLog) error {
	if f.failAttempt {
		return audit.ErrAuditUnavailable
	}
	entry.Status = audit.StatusAttempted
	entry.RequestID = "req-1"
	return f.LogDBEntry(entry)
}

func (f *fakeAuditLog) LogDBEntry(entry *db.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeAuditLog) statuses() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	statuses := make([]string, 0, len(f.entries))
	for _, entry := range f.entries {
		statuses = append(statuses, entry.Status)
	}
	return statuses
}

// fakeCircleCI serves one workflow in pipeline 100 of github.com/DocSpring/docspring at flowSHA, with an on-hold
// approval job. Before it records an approval, it calls onApprove.
type fakeCircleCI struct {
	onApprove func()
	mu        sync.Mutex
	approved  []string
}

func (f *fakeCircleCI) serve(t *testing.T) string {
	t.Helper()
	repo := "https://github.com/DocSpring/docspring"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body interface{}
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/approve/"):
			if f.onApprove != nil {
				f.onApprove()
			}
			f.mu.Lock()
			f.approved = append(f.approved, r.URL.Path)
			f.mu.Unlock()
			body = map[string]string{"message": "Accepted."}
		case r.URL.Path == "/workflow/"+flowWorkflowID:
			body = map[string]string{
				"id": flowWorkflowID, "name": "deploy", "pipeline_id": flowPipelineID,
				"created_at": "2026-10-13T03:00:00Z",
			}
		case r.URL.Path == "/workflow/"+flowWorkflowID+"/job":
			body = map[string]interface{}{"items": []map[string]string{
				{"id": "hold-1", "name": "approve_deploy", "type": "approval", "status": "on_hold"},
			}}
		case r.URL.Path == "/pipeline/"+flowPipelineID:
			body = map[string]interface{}{
				"id": flowPipelineID, "number": 100, "project_slug": "gh/DocSpring/docspring",
				"vcs": map[string]string{
					"revision": flowSHA, "target_repository_url": repo, "origin_repository_url": repo,
				},
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (f *fakeCircleCI) approvals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.approved...)
}

// sequenceApprovals answers each lookup of the deploy approval with the next state in turn (the last repeats).
type sequenceApprovals struct {
	mu     sync.Mutex
	states []*db.DeployApprovalRequest
}

func (s *sequenceApprovals) GetDeployApprovalRequest(int64) (*db.DeployApprovalRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.states[0]
	if len(s.states) > 1 {
		s.states = s.states[1:]
	}
	return next, nil
}

func (s *sequenceApprovals) WithDeployApprovalRequestHeld(id int64, fn func(*db.DeployApprovalRequest) error) error {
	held, err := s.GetDeployApprovalRequest(id)
	if err != nil {
		return err
	}
	return fn(held)
}

func flowApproval(status string) *db.DeployApprovalRequest {
	expires := time.Now().Add(time.Hour)
	return &db.DeployApprovalRequest{
		ID: 7, PublicID: "dar-7", App: "web", GitCommitHash: flowSHA, Status: status, ApprovalExpiresAt: &expires,
	}
}

func alwaysApproved() *sequenceApprovals {
	return &sequenceApprovals{states: []*db.DeployApprovalRequest{flowApproval(db.DeployApprovalRequestStatusApproved)}}
}

func flowJob() *river.Job[ApproveJobArgs] {
	return &river.Job[ApproveJobArgs]{JobRow: &rivertype.JobRow{}, Args: ApproveJobArgs{
		WorkflowID: flowWorkflowID, PipelineNumber: "100", ApprovalJobName: "approve_deploy",
		DeployApprovalRequestID: 7, ExpectedRevision: flowSHA, ExpectedRepo: "github.com/DocSpring/docspring",
	}}
}

func newFlowWorker(t *testing.T, fake *fakeCircleCI, approvals ApprovalStore, log *fakeAuditLog) *ApproveJobWorker {
	t.Helper()
	worker := NewApproveJobWorker(approvals, "token", log)
	worker.baseURL = fake.serve(t)
	return worker
}

// The approval is in the audit log, naming the hold job, workflow and pipeline, before it's sent to CircleCI, and
// its result after.
func TestApproveJobRecordsTheApprovalBeforeSendingIt(t *testing.T) {
	log := &fakeAuditLog{}
	fake := &fakeCircleCI{}
	fake.onApprove = func() { require.Equal(t, []string{audit.StatusAttempted}, log.statuses()) }

	require.NoError(t, newFlowWorker(t, fake, alwaysApproved(), log).Work(context.Background(), flowJob()))

	require.Equal(t, []string{"/workflow/" + flowWorkflowID + "/approve/hold-1"}, fake.approvals())
	require.Equal(t, []string{audit.StatusAttempted, audit.StatusSuccess}, log.statuses())
	result := log.entries[1]
	require.Equal(t, "deploy_approval_request.circleci_approve", result.Action)
	require.Equal(t, "req-1", result.RequestID, "the result is paired with its attempt")
	require.Equal(t, int64(7), *result.DeployApprovalRequestID)
	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte(result.Details), &details))
	require.Equal(t, "hold-1", details["job_id"])
	require.Equal(t, flowWorkflowID, details["workflow_id"])
	require.Equal(t, "100", details["pipeline_number"])
}

// Nothing is approved when the attempt can't be written; the job is retried.
func TestApproveJobIsNotSentWithoutAnAuditEntry(t *testing.T) {
	fake := &fakeCircleCI{}
	worker := newFlowWorker(t, fake, alwaysApproved(), &fakeAuditLog{failAttempt: true})
	err := worker.Work(context.Background(), flowJob())
	require.ErrorIs(t, err, audit.ErrAuditUnavailable)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "retried once the audit log is back")
	require.Empty(t, fake.approvals())
}

// A deploy approval rejected while CircleCI was being checked isn't approved: the approval is checked again before
// the attempt is recorded.
func TestApproveJobRechecksTheApprovalBeforeApproving(t *testing.T) {
	fake := &fakeCircleCI{}
	log := &fakeAuditLog{}
	approvals := &sequenceApprovals{states: []*db.DeployApprovalRequest{
		flowApproval(db.DeployApprovalRequestStatusApproved), flowApproval(db.DeployApprovalRequestStatusRejected),
	}}
	err := newFlowWorker(t, fake, approvals, log).Work(context.Background(), flowJob())
	require.ErrorIs(t, err, errApprovalNotActive)
	require.Empty(t, fake.approvals())
	require.Empty(t, log.statuses())
}

// A rejection that lands after the attempt was recorded either waits for the approval (the approval is held while
// it's sent) or wins: the held approval is checked before it's sent, and the attempt's result records the failure.
func TestApproveJobChecksTheHeldApprovalBeforeSendingIt(t *testing.T) {
	fake := &fakeCircleCI{}
	log := &fakeAuditLog{}
	approved := flowApproval(db.DeployApprovalRequestStatusApproved)
	approvals := &sequenceApprovals{states: []*db.DeployApprovalRequest{
		approved, approved, flowApproval(db.DeployApprovalRequestStatusRejected),
	}}
	err := newFlowWorker(t, fake, approvals, log).Work(context.Background(), flowJob())
	require.ErrorIs(t, err, errApprovalNotActive)
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "a rejected approval isn't retried")
	require.Empty(t, fake.approvals())
	require.Equal(t, []string{audit.StatusAttempted, audit.StatusFailed}, log.statuses())
	require.Contains(t, log.entries[1].Details, "is rejected")
}
