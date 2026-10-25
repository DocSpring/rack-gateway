package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/scriptrun"
)

// The limit is on the command as the audit log records it: redacted, then JSON-encoded.
func TestCheckCommandLengthMeasuresTheRecordedCommand(t *testing.T) {
	// A JSON string adds two quotes.
	atLimit := strings.Repeat("x", MaxRecordedCommandBytes-2)
	require.NoError(t, CheckCommandLength(atLimit))

	err := CheckCommandLength(atLimit + "x")
	require.ErrorContains(t, err, "too long for the audit log to keep in full")
	require.ErrorContains(t, err, "51201 bytes as recorded, at most 51200, or 50 KB")

	// Each < is recorded as \u003c, six bytes.
	require.Error(t, CheckCommandLength(strings.Repeat("<", MaxRecordedCommandBytes/6+1)))
}

// A script command is measured as recorded: the command and the script's text, not the base64 sent.
func TestCheckCommandLengthMeasuresARecordedScript(t *testing.T) {
	// "rails runner check.rb" is 23 bytes recorded, and the script's quotes 2 more.
	script := strings.Repeat("x", MaxRecordedCommandBytes-25)
	require.NoError(t, CheckCommandLength(scriptrun.Build("check.rb", []byte(script), "rails runner")))
	require.ErrorContains(t,
		CheckCommandLength(scriptrun.Build("check.rb", []byte(script+"x"), "rails runner")),
		"51201 bytes as recorded")
}

// A script command's details hold the command as a person would run it locally, the script's text with its line
// breaks, its name and its SHA-256. Credentials in the script are redacted like those on a command line.
func TestCommandDetailsRecordAScriptReadably(t *testing.T) {
	// The URL is built from parts: gosec takes a literal one for a hardcoded credential (G101).
	text := "conn = '" + "postgres://app:" + "hunter2@db/app'\nputs Account.count\n"
	details := commandDetails(scriptrun.Build("check.rb", []byte(text), "rails runner"))
	require.Equal(t, "rails runner check.rb", details["command"])
	require.Equal(t, "conn = 'postgres://[REDACTED]@db/app'\nputs Account.count\n", details["script"])
	require.Equal(t, "check.rb", details["script_name"])
	sum := sha256.Sum256([]byte(text))
	require.Equal(t, hex.EncodeToString(sum[:]), details["script_sha256"])

	plain := commandDetails("rails runner 'puts 1'")
	require.Equal(t, map[string]interface{}{"command": "rails runner 'puts 1'"}, plain)
}
