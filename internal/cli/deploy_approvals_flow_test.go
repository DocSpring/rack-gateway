package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const approveFlowRequestID = "6f1c2a8e-4b7d-4e0a-9c3f-1a2b3c4d5e6f"

// lockedBuffer collects CLI output. The fake gateway reads it while the CLI waits for a response.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// seenApproval is an approval the fake gateway received, with the CLI output printed before it.
type seenApproval struct {
	id            string
	authorization string
	outputBefore  string
}

// fakeDeployGateway serves deploy approval requests like the gateway does: listing them (filtered by status and
// app, newest first), fetching one by ID, and approving a pending one.
type fakeDeployGateway struct {
	mu        sync.Mutex
	requests  []deployApprovalRequest
	approvals []seenApproval
	output    *lockedBuffer
}

// startFakeDeployGateway configures the "staging" rack to use a fake gateway serving requests, and passes MFA
// with --mfa-code so approving needs no security key.
func startFakeDeployGateway(t *testing.T, requests ...deployApprovalRequest) *fakeDeployGateway {
	t.Helper()
	gateway := &fakeDeployGateway{requests: requests, output: &lockedBuffer{}}
	server := httptest.NewServer(gateway)
	t.Cleanup(server.Close)

	withCLIConfig(t, Config{NotificationSound: "disabled", Gateways: map[string]GatewayConfig{
		"staging": {URL: server.URL, Token: "session-token", ExpiresAt: time.Now().Add(time.Hour)},
	}})
	previousMFACode := MFACodeFlag
	MFACodeFlag = "123456"
	t.Cleanup(func() { MFACodeFlag = previousMFACode })
	return gateway
}

// command returns a command that writes to the gateway's output buffer and reads its input from in.
func (g *fakeDeployGateway) command(in string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(g.output)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(in))
	return cmd
}

func (g *fakeDeployGateway) seen() []seenApproval {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]seenApproval(nil), g.approvals...)
}

func (g *fakeDeployGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/deploy-approval-requests")
	id, approve := strings.CutSuffix(strings.TrimPrefix(path, "/"), "/approve")
	switch {
	case r.Method == http.MethodGet && path == "":
		g.list(w, r.URL.Query())
	case r.Method == http.MethodGet && !approve && g.find(id) != nil:
		respondJSON(w, http.StatusOK, g.find(id))
	case r.Method == http.MethodPost && approve:
		g.approve(w, r, id)
	default:
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "deploy approval request not found"})
	}
}

// list pages through the matching requests, at most 200 at a time, keeping only each app's newest when asked.
func (g *fakeDeployGateway) list(w http.ResponseWriter, query url.Values) {
	matches := []deployApprovalRequest{}
	seen := map[string]bool{}
	for _, req := range g.requests {
		app := query.Get("app")
		if req.Status != query.Get("status") || (app != "" && req.App != app) {
			continue
		}
		if query.Get("newest_per_app") == "true" && seen[req.App] {
			continue
		}
		seen[req.App] = true
		matches = append(matches, req)
	}
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	matches = matches[min(offset, len(matches)):min(offset+limit, len(matches))]
	respondJSON(w, http.StatusOK, deployApprovalRequestList{DeployApprovalRequests: matches})
}

func (g *fakeDeployGateway) approve(w http.ResponseWriter, r *http.Request, id string) {
	req := g.find(id)
	if req == nil || req.Status != "pending" {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "deploy approval request not found"})
		return
	}
	g.approvals = append(g.approvals, seenApproval{
		id: id, authorization: r.Header.Get("Authorization"), outputBefore: g.output.String(),
	})
	expiresAt := time.Now().Add(time.Hour)
	req.Status = "approved"
	req.ApprovalExpiresAt = &expiresAt
	respondJSON(w, http.StatusOK, req)
}

func (g *fakeDeployGateway) find(id string) *deployApprovalRequest {
	for i := range g.requests {
		if g.requests[i].PublicID == id {
			return &g.requests[i]
		}
	}
	return nil
}

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// shownRequest is a request with every field the CLI shows before approving it.
func shownRequest(id, app, status string) deployApprovalRequest {
	req := approvalRequest(id, app, status)
	req.Message = "Deploy " + app
	req.GitBranch = "main"
	req.TargetAPITokenID = "token-1"
	req.TargetAPITokenName = "ci-deployer"
	return req
}

func TestApproveByIDShowsTheRequestBeforeApproving(t *testing.T) {
	request := shownRequest(approveFlowRequestID, "web", "pending")
	request.Message = "Deploy web\x1b[2J"
	gateway := startFakeDeployGateway(t, request)

	require.NoError(t, approveByID(gateway.command("\n"), []string{"staging"}, approveFlowRequestID, ""))

	approvals := gateway.seen()
	require.Len(t, approvals, 1)
	require.Equal(t, approveFlowRequestID, approvals[0].id)
	require.True(t, strings.HasSuffix(approvals[0].authorization, ".totp.123456"), "approved with MFA")
	shown := approvals[0].outputBefore
	for _, want := range []string{"staging", "web", appsTestCommit, "main", "ci-deployer", "Deploy web?[2J"} {
		require.Contains(t, shown, want, "the request is shown before it is approved")
	}
	require.NotContains(t, shown, "\x1b[2J", "CI-supplied text is shown safely")
	require.Contains(t, gateway.output.String(), approveFlowRequestID+" (web, commit 36a302d01234) approved")
}

func TestApproveByIDOnlyApprovesPendingRequests(t *testing.T) {
	tests := map[string]string{"approved": "", "deployed": "", "rejected": "is rejected", "expired": "is expired"}
	for status, wantErr := range tests {
		t.Run(status, func(t *testing.T) {
			gateway := startFakeDeployGateway(t, shownRequest(approveFlowRequestID, "web", status))

			err := approveByID(gateway.command("\n"), []string{"staging"}, approveFlowRequestID, "")
			if wantErr == "" {
				require.NoError(t, err)
				require.Contains(t, gateway.output.String(), "Nothing to approve")
			} else {
				require.ErrorContains(t, err, wantErr)
			}
			require.Contains(t, gateway.output.String(), appsTestCommit, "the request is shown either way")
			require.Empty(t, gateway.seen())
		})
	}
}

func TestApproveByIDReportsAnUnknownRequest(t *testing.T) {
	gateway := startFakeDeployGateway(t)

	err := approveByID(gateway.command("\n"), []string{"staging"}, approveFlowRequestID, "")
	require.ErrorContains(t, err, "deploy approval request "+approveFlowRequestID+" not found")
	require.Empty(t, gateway.seen())
}

func TestApproveBySearchShowsEveryRequestBeforeApproving(t *testing.T) {
	gateway := startFakeDeployGateway(t,
		shownRequest("p-web", "web", "pending"),
		shownRequest("p-worker", "worker", "pending"),
	)
	apps := approvalApps{names: []string{"web", "worker"}}

	err := approveBySearch(gateway.command("\n"), []string{"staging"}, apps, "", appsTestCommit, "")
	require.NoError(t, err)

	approvals := gateway.seen()
	require.Len(t, approvals, 2)
	for _, want := range []string{"p-web", "p-worker", "Deploy web", "Deploy worker", appsTestCommit} {
		require.Contains(t, approvals[0].outputBefore, want, "every request is shown before the first approval")
	}
}

// The search reads every page of requests: an app whose request comes after the gateway's first 200 still counts.
func TestApproveBySearchReadsEveryPage(t *testing.T) {
	requests := make([]deployApprovalRequest, 0, 251)
	for i := range 250 {
		requests = append(requests, shownRequest(fmt.Sprintf("d-%03d", i), fmt.Sprintf("app-%03d", i), "deployed"))
	}
	requests = append(requests, shownRequest("p-web", "web", "pending"))
	gateway := startFakeDeployGateway(t, requests...)

	err := approveBySearch(gateway.command("\n"), []string{"staging"}, approvalApps{all: true}, "", appsTestCommit, "")
	require.NoError(t, err)
	approvals := gateway.seen()
	require.Len(t, approvals, 1)
	require.Equal(t, "p-web", approvals[0].id)
	require.Contains(t, approvals[0].outputBefore, "d-249", "every app's request is shown")
}

// An approval past its expiry no longer allows a deploy: the search doesn't report it as done.
func TestApproveBySearchDoesNotCountAnExpiredApproval(t *testing.T) {
	expired := shownRequest("a-web", "web", "approved")
	expiredAt := time.Now().Add(-time.Minute)
	expired.ApprovalExpiresAt = &expiredAt
	gateway := startFakeDeployGateway(t, expired)

	err := approveBySearch(gateway.command("\n"), []string{"staging"},
		approvalApps{names: []string{"web"}}, "", appsTestCommit, "")
	require.ErrorContains(t, err, "no deploy approval request found for commit "+appsTestCommit+" on staging/web")
	require.NotContains(t, gateway.output.String(), "Nothing to approve")
}

func TestApprovalSummaryDescribesEachApproval(t *testing.T) {
	web := shownRequest("p-web", "web", "pending")
	web.Message = "Deploy web\nwith a second line"
	unnamed := shownRequest("p-unnamed", "", "pending")

	got := approvalSummary([]rackApproval{{rack: "staging", req: &web}, {rack: "us", req: &unnamed}})
	require.Equal(t, []string{
		`web on staging, commit 36a302d01234: "Deploy web?with a second line"`,
		`request p-unnamed on us, commit 36a302d01234: "Deploy "`,
	}, got)
}

func TestShortCommit(t *testing.T) {
	require.Equal(t, "36a302d01234", shortCommit(appsTestCommit))
	require.Equal(t, "36a302d", shortCommit("36a302d"))
	require.Equal(t, "(none)", shortCommit(""))
	require.Equal(t, "?[2J", shortCommit("\x1b[2J"))
}
