package proxy

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
)

// maskedProtectedEnv is the start of what the CLI posts for the protected env fixture's app: its protected keys
// masked, as the gateway served them.
const maskedProtectedEnv = "DATABASE_URL=" + maskedSecret + "\nADMIN_PASSWORD=" + maskedSecret + "\nPORT=3000"

func TestParsePostedEnv(t *testing.T) {
	posted, order, err := parsePostedEnv("PORT=3000\n\n  \nFLAG\n URL = a=b \nPORT=4000\n")
	require.NoError(t, err)
	require.Equal(t, []string{"PORT", "FLAG", "URL"}, order)
	require.Equal(t, map[string]string{"PORT": "4000", "FLAG": "", "URL": " a=b "}, posted)
}

// Keys must be env var names, and values can't hold control characters: the rack stores them, and `convox env`
// prints them to other users' terminals.
func TestParsePostedEnvRefusesInvalidEntries(t *testing.T) {
	for name, env := range map[string]string{
		"escape sequence in value": "PORT=3000\nBANNER=hi\x1b[2J",
		"NUL in value":             "PORT=3000\x00",
		"carriage return in value": "PORT=3000\r",
		"control character in key": "PO\x1bRT=3000",
		"empty key":                "=3000",
		"space in key":             "MY PORT=3000",
	} {
		_, _, err := parsePostedEnv(env)
		require.ErrorIsf(t, err, envutil.ErrInvalidEnvEntry, "%s", name)
	}
}

// The env is parsed before any permission check, so its size is checked before it is parsed: an env of millions of
// blank lines, or of many short ones, is refused instead of costing memory per line.
func TestParsePostedEnvRefusesOversizedEnvs(t *testing.T) {
	var shortLines strings.Builder
	for i := range 20000 {
		_, _ = fmt.Fprintf(&shortLines, "VAR_%d=x\n", i)
	}
	for name, env := range map[string]string{
		"2 MB of newlines":   strings.Repeat("\n", 2<<20),
		"20,000 short lines": shortLines.String(),
		"over 1 MiB":         "BIG=" + strings.Repeat("x", maxPostedEnvBytes),
	} {
		_, _, err := parsePostedEnv(env)
		require.ErrorIsf(t, err, errBodyTooLarge, "%s", name)
	}

	_, order, err := parsePostedEnv(strings.Repeat("\n", maxPostedEnvLines-1) + "PORT=3000")
	require.NoError(t, err, "an env of exactly the line limit is accepted")
	require.Equal(t, []string{"PORT"}, order)
}

// Within the limits, parsing allocates per entry, not per line: blank lines cost nothing. A slice entry per line
// would be 16 bytes for each of these 10,000 lines, 160 KB a run.
func TestParsePostedEnvMemoryDoesNotGrowWithBlankLines(t *testing.T) {
	env := strings.Repeat("\n", maxPostedEnvLines-1) + "PORT=3000"
	const runs = 50
	var (
		before, after runtime.MemStats
		order         []string
		err           error
	)
	runtime.ReadMemStats(&before)
	for range runs {
		_, order, err = parsePostedEnv(env)
	}
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	require.Equal(t, []string{"PORT"}, order)
	perRun := (after.TotalAlloc - before.TotalAlloc) / runs
	require.Lessf(t, perRun, uint64(16<<10), "parsing allocated %d bytes a run", perRun)
}

// A release create whose env holds an invalid entry is refused before it reaches the rack.
func TestReleaseCreateRefusesInvalidEnvEntries(t *testing.T) {
	f := newProtectedEnvFixture(t)
	for _, line := range []string{"BANNER=hi\x1b[2J", "NOTE=a\x00b", "BAD\x1bKEY=x"} {
		req, _ := f.request(t, f.deployer, maskedProtectedEnv+"\n"+line, "", "", stepUpAgeFresh)
		rr := f.proxy(req)
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Contains(t, rr.Body.String(), envutil.ErrInvalidEnvEntry.Error())
	}
	require.Empty(t, f.rack.releases())
}

func TestReleaseCreateRefusesOversizedEnv(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, strings.Repeat("\n", 2<<20), "", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "the release env is over the gateway's limit")
	require.Empty(t, f.rack.releases())
}

// A masked value keeps the key's current value. For a key the app doesn't have, it would create the key empty,
// past the secret and protected checks (which skip masked values) and without an audit entry.
func TestReleaseCreateRefusesMaskedValueForMissingKey(t *testing.T) {
	f := newProtectedEnvFixture(t)
	req, _ := f.request(t, f.deployer, maskedProtectedEnv+"\nAPI_TOKEN="+maskedSecret, "", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "API_TOKEN is masked, but has no current value to keep")
	require.Empty(t, f.rack.releases())
}
