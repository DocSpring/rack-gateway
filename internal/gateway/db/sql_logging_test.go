package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/debuglog"
)

// The sql debug topic logs every query, so it mustn't log the values that could be secret: a CLI login's state
// and PKCE verifier, token hashes, encrypted MFA secrets.
func TestSQLDebugLogShowsNoStringArguments(t *testing.T) {
	logs := debuglog.Capture(t, "sql")
	var database *Database

	database.logQuery("QUERY:", "SELECT id FROM cli_login_states WHERE state = ? AND oauth_code_verifier = ?",
		"hidden-login-state", "hidden-code-verifier")

	out := logs.String()
	require.Contains(t, out, "cli_login_states")
	require.Contains(t, out, "args=[<string len=18>, <string len=20>]")
	require.NotContains(t, out, "hidden-")
}

func TestFormatArgsShowsOnlyValuesThatCantBeSecret(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	args := []interface{}{
		nil, int64(42), true, 1.5, at, "hidden-login-state", []byte("abc"),
		sql.NullString{String: "hidden", Valid: true},
	}

	require.Equal(t,
		"[NULL, 42, true, 1.5, 2026-01-02T03:04:05Z, <string len=18>, <bytes len=3>, <sql.NullString>]",
		formatArgs(args))
	require.Equal(t, "[]", formatArgs(nil))
}
