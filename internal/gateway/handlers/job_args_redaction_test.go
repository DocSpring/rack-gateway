package handlers

import (
	"encoding/json"
	"testing"

	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactJobArgs(t *testing.T) {
	raw := json.RawMessage(`{
		"circleci_token": "circle-secret",
		"github_token": "ghp_secret",
		"workflow_id": "11111111-2222-3333-4444-555555555555",
		"nested": {"api_key": "k", "password": "p", "count": 3},
		"items": [{"client_secret": "s", "name": "ok"}]
	}`)

	redacted := redactJobArgs(raw)
	require.NotNil(t, redacted)
	assert.NotContains(t, string(redacted), "circle-secret")
	assert.NotContains(t, string(redacted), "ghp_secret")

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(redacted, &decoded))
	assert.Equal(t, redactedJobArg, decoded["circleci_token"])
	assert.Equal(t, redactedJobArg, decoded["github_token"])
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", decoded["workflow_id"])
	nested := decoded["nested"].(map[string]interface{})
	assert.Equal(t, redactedJobArg, nested["api_key"])
	assert.Equal(t, redactedJobArg, nested["password"])
	assert.InDelta(t, 3, nested["count"], 0)
	item := decoded["items"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, redactedJobArg, item["client_secret"])
	assert.Equal(t, "ok", item["name"])
}

func TestRedactJobArgsWithholdsUnparseableArgs(t *testing.T) {
	assert.Nil(t, redactJobArgs(json.RawMessage(`not json "token":"x"`)))
	assert.Nil(t, redactJobArgs(nil))
}

// Every jobs API response goes through toJobResponse, so job args never reach the client unredacted.
func TestJobResponseRedactsArgs(t *testing.T) {
	job := &rivertype.JobRow{
		ID: 1, Kind: "circleci:approve_job", State: rivertype.JobStateAvailable,
		EncodedArgs: []byte(`{"circleci_token":"leaked","workflow_id":"w"}`),
	}
	encoded, err := json.Marshal(toJobResponse(job).Args)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "leaked")
	assert.Contains(t, string(encoded), `"workflow_id":"w"`)
}
