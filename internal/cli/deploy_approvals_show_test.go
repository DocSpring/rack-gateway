package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const showTestPRURL = "https://github.com/acme/web/pull/7"

// requestWithPR is a pending request whose branch has an open pull request.
func requestWithPR() deployApprovalRequest {
	req := shownRequest(approveFlowRequestID, "web", "pending")
	req.PrURL = showTestPRURL
	return req
}

func TestShowPrintsThePRURL(t *testing.T) {
	gateway := startFakeDeployGateway(t, requestWithPR())

	require.NoError(t, showByID(gateway.command(""), []string{"staging"}, approveFlowRequestID, ""))
	require.Contains(t, gateway.output.String(), "PR:")
	require.Contains(t, gateway.output.String(), showTestPRURL)
}

func TestShowJSONIncludesThePRURL(t *testing.T) {
	gateway := startFakeDeployGateway(t, requestWithPR())

	require.NoError(t, showByID(gateway.command(""), []string{"staging"}, approveFlowRequestID, "json"))
	var shown map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(gateway.output.String()), &shown))
	require.Equal(t, showTestPRURL, shown["pr_url"])
}

func TestListJSONIncludesThePRURL(t *testing.T) {
	gateway := startFakeDeployGateway(t, requestWithPR())
	previousRack := RackFlag
	RackFlag = "staging"
	t.Cleanup(func() { RackFlag = previousRack })

	opts := deployApprovalListOptions{status: "pending", output: "json"}
	require.NoError(t, executeDeployApprovalList(gateway.command(""), opts))
	var listed struct {
		DeployApprovalRequests []map[string]interface{} `json:"deploy_approval_requests"`
	}
	require.NoError(t, json.Unmarshal([]byte(gateway.output.String()), &listed))
	require.Len(t, listed.DeployApprovalRequests, 1)
	require.Equal(t, showTestPRURL, listed.DeployApprovalRequests[0]["pr_url"])
}

// requestWithEscapes is a pending request whose gateway-supplied ID, status and token ID carry escape sequences.
func requestWithEscapes() deployApprovalRequest {
	req := shownRequest(approveFlowRequestID+"\x1b[2J", "web", "pending\x1b]0;approved\x07")
	req.TargetAPITokenID = "token-1\u009b31m"
	req.TargetAPITokenName = ""
	return req
}

// rawEscapes are the sequences requestWithEscapes carries. Labels may be colored when stdout is a terminal, so the
// tests look for these rather than for any ESC.
var rawEscapes = []string{"\x1b[2J", "\x1b]0;", "\x07", "\u009b"}

func TestShowNeutralizesEveryGatewayField(t *testing.T) {
	output := &lockedBuffer{}
	req := requestWithEscapes()

	require.NoError(t, printDeployApprovalDetails(output, &req, "staging", true))
	shown := output.String()
	for _, want := range []string{approveFlowRequestID + "?[2J", "pending?]0;approved?", "token-1?31m"} {
		require.Contains(t, shown, want)
	}
	for _, raw := range rawEscapes {
		require.NotContains(t, shown, raw)
	}
}

func TestListNeutralizesEveryGatewayField(t *testing.T) {
	req := requestWithEscapes()
	gateway := startFakeDeployGateway(t, req)
	previousRack := RackFlag
	RackFlag = "staging"
	t.Cleanup(func() { RackFlag = previousRack })

	require.NoError(t, executeDeployApprovalList(gateway.command(""), deployApprovalListOptions{status: req.Status}))
	listed := gateway.output.String()
	for _, want := range []string{approveFlowRequestID + "?[2J", "pending?]0;approved?", "token-1?31m"} {
		require.Contains(t, listed, want, "the table is written to the command's output, made safe")
	}
	for _, raw := range rawEscapes {
		require.NotContains(t, listed, raw)
	}
}

// A request without an app is named by its ID in the touch prompt (printed on the terminal) and the PIN dialog.
func TestApprovalTouchAndSummaryNeutralizeTheRequestID(t *testing.T) {
	req := &deployApprovalRequest{PublicID: "req-1\x1b[2J", GitCommitHash: appsTestCommit, Message: "Deploy"}

	require.Equal(t, "approve request req-1?[2J on us", approvalTouch("us", req, 0, 0))
	require.Equal(t, `request req-1?[2J on us, commit 36a302d01234: "Deploy"`, approvalSummaryLine("us", req))
}
