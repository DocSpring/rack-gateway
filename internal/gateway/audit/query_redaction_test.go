package audit

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// An audit entry keeps a request's query without its credentials (see debugredact.RedactQuery): a login code or
// OAuth state, and any parameter named for a credential, whether or not the request is refused for it.
func TestBuildDetailsJSONRedactsQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet,
		"/api/v1/auth/cli/callback?code=c0de&state=s3cr3t&api_key=k3y&client_secret=sh&app=web&limit=10", http.NoBody)
	require.NoError(t, err)

	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte((&Logger{}).BuildDetailsJSON(req)), &details))
	query, err := url.ParseQuery(details["query"])
	require.NoError(t, err)
	for _, name := range []string{"code", "state", "api_key", "client_secret"} {
		require.Equal(t, "[REDACTED]", query.Get(name), name)
	}
	require.Equal(t, "web", query.Get("app"))
	require.Equal(t, "10", query.Get("limit"))
}
