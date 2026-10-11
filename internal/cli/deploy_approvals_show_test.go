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
