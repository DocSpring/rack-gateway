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
		{"curl -H 'X_API_KEY: abc123' x", "curl -H 'X_API_KEY: [REDACTED]' x"},
		{"curl -H X_API_KEY:abc123 x", "curl -H X_API_KEY:[REDACTED] x"},
		{`curl -H Authorization:\ Bearer\ abc123 x`, `curl -H Authorization:[REDACTED] x`},
		{`tool --token a\ b --verbose`, `tool --token [REDACTED] --verbose`},
		{`echo "a \" b" PASSWORD=x trailing\`, `echo "a \" b" PASSWORD=[REDACTED] trailing\`},
		{"mysql -uroot -ps3cret app", "mysql -uroot -p[REDACTED] app"},
		{"mysql -uroot -p app", "mysql -uroot -p app"},
		{"/usr/bin/mysqldump -h db -p'two words' app", "/usr/bin/mysqldump -h db -p'[REDACTED]' app"},
		{"redis-cli -h cache -a s3cret ping", "redis-cli -h cache -a [REDACTED] ping"},
		{"sshpass -p s3cret ssh host", "sshpass -p [REDACTED] ssh host"},
		{"mongosh -u admin -p s3cret db", "mongosh -u admin -p [REDACTED] db"},
		{"curl -u admin:s3cret https://x", "curl -u admin:[REDACTED] https://x"},
		{"curl --user 'admin:two words' https://x", "curl --user 'admin:[REDACTED]' https://x"},
		{"curl -uadmin:s3cret -u admin https://x", "curl -uadmin:[REDACTED] -u admin https://x"},
		{"sh -c 'mysql -ps3cret app'", "sh -c 'mysql -p[REDACTED] app'"},
		{"psql -p 5432 -h db && ssh -p 22 host", "psql -p 5432 -h db && ssh -p 22 host"},
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
