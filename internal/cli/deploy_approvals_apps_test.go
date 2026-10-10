package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const appsTestCommit = "36a302d0123456789abcdef0123456789abcdef0"

func TestResolveApprovalApps(t *testing.T) {
	apps, err := resolveApprovalApps(" docspring, api-proxy,docspring ", false)
	require.NoError(t, err)
	require.Equal(t, []string{"api-proxy", "docspring"}, apps.names)
	require.True(t, apps.includes("api-proxy"))
	require.False(t, apps.includes("worker"))
	require.Empty(t, apps.queryApp(), "several apps are filtered client-side")

	single, err := resolveApprovalApps("docspring", false)
	require.NoError(t, err)
	require.Equal(t, "docspring", single.queryApp())

	all, err := resolveApprovalApps("", true)
	require.NoError(t, err)
	require.True(t, all.includes("anything"))
	require.Equal(t, "all apps", all.String())

	_, err = resolveApprovalApps("docspring", true)
	require.ErrorContains(t, err, "either --app or --all-apps")
	_, err = resolveApprovalApps("docspring,,api-proxy", false)
	require.ErrorContains(t, err, "empty app name")
}

// fakeApprovalGateway serves GET /api/v1/deploy-approval-requests from a fixed list, applying the status and
// app filters like the gateway does (newest first).
func fakeApprovalGateway(t *testing.T, requests []deployApprovalRequest) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/deploy-approval-requests" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status, app := r.URL.Query().Get("status"), r.URL.Query().Get("app")
		matches := []deployApprovalRequest{}
		for _, req := range requests {
			if req.Status == status && (app == "" || req.App == app) {
				matches = append(matches, req)
			}
		}
		_ = json.NewEncoder(w).Encode(deployApprovalRequestList{DeployApprovalRequests: matches})
	}))
	t.Cleanup(server.Close)

	// Sound off: finding a pending request plays the deploy-approval bell.
	withCLIConfig(t, Config{NotificationSound: "disabled", Gateways: map[string]GatewayConfig{
		"staging": {URL: server.URL, Token: "session-token", ExpiresAt: time.Now().Add(time.Hour)},
	}})
}

func approvalRequest(id, app, status string) deployApprovalRequest {
	return deployApprovalRequest{PublicID: id, App: app, Status: status, GitCommitHash: appsTestCommit}
}

func TestCollectAllRequestsFindsEveryAppAndReportsMissingOnes(t *testing.T) {
	fakeApprovalGateway(t, []deployApprovalRequest{
		approvalRequest("p-docspring", "docspring", "pending"),
		approvalRequest("a-api-proxy", "api-proxy", "approved"),
		approvalRequest("p-other", "other-app", "pending"),
	})

	apps := approvalApps{names: []string{"api-proxy", "docspring", "worker"}}
	found, missing, err := collectAllRequests(&cobra.Command{}, []string{"staging"}, apps, "", appsTestCommit)
	require.NoError(t, err)
	require.Equal(t, []string{"staging/worker"}, missing)
	ids := make([]string, 0, len(found))
	for _, r := range found {
		ids = append(ids, r.req.PublicID)
	}
	require.ElementsMatch(t, []string{"p-docspring", "a-api-proxy"}, ids, "other apps are out of scope")

	// Nothing is approved when an expected request is missing.
	err = approveBySearch(&cobra.Command{}, []string{"staging"}, apps, "", appsTestCommit, "")
	require.ErrorContains(t, err, "on staging/worker; nothing was approved")

	all, missing, err := collectAllRequests(
		&cobra.Command{}, []string{"staging"}, approvalApps{all: true}, "", appsTestCommit,
	)
	require.NoError(t, err)
	require.Empty(t, missing)
	require.Len(t, all, 3)
}

func TestWaitWaitsForEveryListedAppOnEveryRack(t *testing.T) {
	fakeApprovalGateway(t, []deployApprovalRequest{approvalRequest("p-docspring", "docspring", "pending")})
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})

	waiter := newDeployApprovalWaiter(cmd, deployApprovalWaitConfig{
		racks: []string{"staging"}, apps: approvalApps{names: []string{"api-proxy", "docspring"}},
		commit: appsTestCommit, pollInterval: time.Millisecond,
	})
	done, err := waiter.pollNextRack()
	require.NoError(t, err)
	require.False(t, done, "api-proxy's request hasn't appeared yet")
	require.Equal(t, "p-docspring", waiter.handled["staging"]["docspring"])
}

// Once promote marks requests deployed, an app that already deployed for the commit counts as done.
func TestDeployedRequestsCountAsDone(t *testing.T) {
	fakeApprovalGateway(t, []deployApprovalRequest{
		approvalRequest("d-docspring", "docspring", "deployed"),
		approvalRequest("a-api-proxy", "api-proxy", "approved"),
	})
	apps := approvalApps{names: []string{"api-proxy", "docspring"}}

	found, missing, err := collectAllRequests(&cobra.Command{}, []string{"staging"}, apps, "", appsTestCommit)
	require.NoError(t, err)
	require.Empty(t, missing)
	require.Len(t, found, 2)
	require.Empty(t, filterPendingRequests(found), "deployed and approved requests are never approved again")

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	waiter := newDeployApprovalWaiter(cmd, deployApprovalWaitConfig{
		racks: []string{"staging"}, apps: apps, commit: appsTestCommit, pollInterval: time.Millisecond,
	})
	done, err := waiter.pollNextRack()
	require.NoError(t, err)
	require.True(t, done)
}

func TestApprovalWithoutTerminalMustNameAppsAndFullCommit(t *testing.T) {
	require.NoError(t, requireExplicitApprovalTarget(true, "", false, "", ""), "a terminal shows the requests")
	require.NoError(t, requireExplicitApprovalTarget(false, "docspring,api-proxy", false, "", appsTestCommit))
	for name, args := range map[string][4]string{
		"implicit app":  {"", "", "", appsTestCommit},
		"short commit":  {"docspring", "", "", appsTestCommit[:7]},
		"implicit HEAD": {"docspring", "", "", ""},
		"branch":        {"docspring", "", "main", appsTestCommit},
		"all apps":      {"", "all", "", appsTestCommit},
		"not a SHA":     {"docspring", "", "", strings.Repeat("g", 40)},
	} {
		err := requireExplicitApprovalTarget(false, args[0], args[1] == "all", args[2], args[3])
		require.ErrorIs(t, err, errApprovalTargetNotExplicit, name)
	}
}

func TestDisplayTextNeutralizesControlAndBidiCharacters(t *testing.T) {
	require.Equal(t, "Deploy 36a302d to staging", displayText("Deploy 36a302d to staging"))
	require.Equal(t, "?[2J?[31mevil", displayText("\x1b[2J\x1b[31mevil"))
	require.Equal(t, "line?break", displayText("line\nbreak"))
	require.Equal(t, "?gnp.exe", displayText("\u202egnp.exe"))
}

// The gateway answers a duplicate request with the open request; the CLI must read it from the 409 body.
func TestRequestConflictIsDecodedFromThe409Body(t *testing.T) {
	existingApp := "api-proxy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(deployApprovalRequest{
			PublicID: "existing-id", App: existingApp, GitCommitHash: appsTestCommit, Status: "pending",
		})
	}))
	t.Cleanup(server.Close)
	withCLIConfig(t, Config{NotificationSound: "disabled", Gateways: map[string]GatewayConfig{
		"staging": {URL: server.URL, Token: "session-token", ExpiresAt: time.Now().Add(time.Hour)},
	}})

	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cfg := deployApprovalRequestConfig{
		rack: "staging", app: "api-proxy", gitCommitHash: appsTestCommit, message: "Deploy api-proxy",
	}
	require.NoError(t, executeDeployApprovalRequest(cmd, cfg))
	require.Contains(t, out.String(), "existing-id already exists for api-proxy")

	existingApp = "docspring"
	err := executeDeployApprovalRequest(cmd, cfg)
	require.ErrorContains(t, err, "nothing was created")
}

func TestRequestConflictOnlySucceedsForTheSameAppAndCommit(t *testing.T) {
	cfg := deployApprovalRequestConfig{app: "api-proxy", gitCommitHash: appsTestCommit}
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	sameApp := &deployApprovalRequestConflictError{request: &deployApprovalRequest{
		PublicID: "existing", App: "api-proxy", GitCommitHash: appsTestCommit, Status: "pending",
	}}
	require.NoError(t, handleDeployApprovalCreationError(cmd, cfg, sameApp))
	require.Contains(t, out.String(), "existing")

	otherApp := &deployApprovalRequestConflictError{request: &deployApprovalRequest{
		PublicID: "docspring-request", App: "docspring", GitCommitHash: appsTestCommit, Status: "pending",
	}}
	err := handleDeployApprovalCreationError(cmd, cfg, otherApp)
	require.ErrorContains(t, err, `"docspring"`)
	require.ErrorContains(t, err, "nothing was created")
}
