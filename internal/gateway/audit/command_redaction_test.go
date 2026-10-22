package audit

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The credentials a command line commonly carries are redacted; the rest of the command is kept as it was.
func TestRedactCommand(t *testing.T) {
	for _, tc := range []struct{ command, want string }{
		{"bin/rails db:migrate", "bin/rails db:migrate"},
		{"psql postgres://admin:hunter2@db/app -c 'x'", "psql postgres://[REDACTED]@db/app -c 'x'"},
		{"env DB_PASSWORD=hunter2  ./run", "env DB_PASSWORD=[REDACTED]  ./run"},
		{"tool --api-key=abc --token xyz --verbose", "tool --api-key=[REDACTED] --token [REDACTED] --verbose"},
		{`tool --secret "two words" --password='a b'`, `tool --secret "[REDACTED]" --password='[REDACTED]'`},
		{`echo "unmatched`, `echo "unmatched`},
		{"tool Bearer abc123 --verbose", "tool Bearer [REDACTED] --verbose"},
		{`curl -H "Authorization: Bearer abc123" https://x`, `curl -H "Authorization: [REDACTED]" https://x`},
		{"curl -H 'X-API-Key: secret' https://x", "curl -H 'X-API-Key: [REDACTED]' https://x"},
		{"curl -H X-Auth-Token:abc https://x", "curl -H X-Auth-Token:[REDACTED] https://x"},
		{`curl -H 'Cookie: session=abc; theme=dark' x`, `curl -H 'Cookie: [REDACTED]' x`},
		{`sh -c 'curl -H "Private-Token: abc" x'`, `sh -c 'curl -H "Private-Token: [REDACTED]" x'`},
		{"bin/rails credentials:edit", "bin/rails credentials:edit"},
		{"curl -H 'Accept: application/json' https://x", "curl -H 'Accept: application/json' https://x"},
		{"sh -c 'PGPASSWORD=s3cret psql -h db'", "sh -c 'PGPASSWORD=[REDACTED] psql -h db'"},
		{`psql "host=db password=xyz"`, `psql "host=db password=[REDACTED]"`},
		{"curl https://example.com/a@b?token=abc", "curl https://example.com/a@b?token=[REDACTED]"},
		{"tool --token", "tool --token"},
		{"echo PASSWORD=", "echo PASSWORD="},
	} {
		require.Equal(t, tc.want, RedactCommand(tc.command), tc.command)
	}
}

// An exec's command is redacted before it goes into the audit record.
func TestBuildDetailsJSONRedactsTheExecCommand(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/apps/web/processes/p-1/exec", nil)
	require.NoError(t, err)
	req.Header.Set("Command", "psql postgres://admin:hunter2@db/app")

	var details map[string]string
	require.NoError(t, json.Unmarshal([]byte((&Logger{}).BuildDetailsJSON(req)), &details))
	require.Equal(t, "psql postgres://[REDACTED]@db/app", details["command"])
	require.Equal(t, "p-1", details["process_id"])
}
