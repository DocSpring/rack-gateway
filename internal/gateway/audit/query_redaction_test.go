package audit

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactQueryHidesLoginSecrets(t *testing.T) {
	redacted := RedactQuery("code=4%2F0AQSTgQ-googlecode&state=SECRET-STATE&login_code=abc&app=web&limit=10")
	values, err := url.ParseQuery(redacted)
	require.NoError(t, err)
	require.Equal(t, "[REDACTED]", values.Get("code"))
	require.Equal(t, "[REDACTED]", values.Get("state"))
	require.Equal(t, "[REDACTED]", values.Get("login_code"))
	require.Equal(t, "web", values.Get("app"))
	require.Equal(t, "10", values.Get("limit"))
	require.NotContains(t, redacted, "SECRET-STATE")
	require.NotContains(t, redacted, "googlecode")
}

func TestRedactQueryEdgeCases(t *testing.T) {
	require.Empty(t, RedactQuery(""))
	require.Equal(t, "[UNPARSEABLE]", RedactQuery("state=%zz"))
	require.Equal(t, "STATE=%5BREDACTED%5D", RedactQuery("STATE=x"))
}

func TestBuildDetailsJSONRedactsQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/api/v1/auth/cli/callback?code=c0de&state=s3cr3t", http.NoBody)
	require.NoError(t, err)
	details := (&Logger{}).BuildDetailsJSON(req)
	require.NotContains(t, details, "c0de")
	require.NotContains(t, details, "s3cr3t")
}
