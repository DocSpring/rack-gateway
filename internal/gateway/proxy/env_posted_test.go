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

// A key with a control character is refused while parsing: the permission checks name keys in audit entries
// before the current env is fetched. Names and values are otherwise checked against the current env.
func TestParsePostedEnvRefusesControlCharactersInKeys(t *testing.T) {
	for _, env := range []string{"PO\x1bRT=3000", "PORT=3000\nA\x00B=1", "K\xff=1"} {
		_, _, err := parsePostedEnv(env)
		require.ErrorIsf(t, err, envutil.ErrInvalidEnvEntry, "%q", env)
	}
	_, order, err := parsePostedEnv("spring.profiles.active=prod\nBANNER=hi\x1b[1m")
	require.NoError(t, err)
	require.Equal(t, []string{"spring.profiles.active", "BANNER"}, order)
}

// A new or changed entry needs an env var name and a value without control characters: the rack stores it, and
// `convox env` prints it to other users' terminals. Entries the current env already has are left alone, so an app
// whose env has a name the gateway wouldn't accept can still change its other keys.
func TestValidateChangedEntries(t *testing.T) {
	base := map[string]string{"spring.profiles.active": "prod", "BANNER": "hi\x1b[1m", "API_TOKEN": "t"}
	unchanged := "PORT=3000\nspring.profiles.active=prod\nBANNER=hi\x1b[1m\nAPI_TOKEN=" + maskedSecret
	require.NoError(t, validateChangedEntries(unchanged, base))

	for env, line := range map[string]int{
		"PORT=3000\nBANNER=hi\x1b[2J": 2,
		"spring.profiles.active=dev":  1,
		"PORT=3000\x00":               1,
		"PORT=3000\r":                 1,
		"PORT=3000\n\n=3000":          3,
		"MY PORT=3000":                1,
	} {
		err := validateChangedEntries(env, base)
		require.ErrorIsf(t, err, envutil.ErrInvalidEnvEntry, "%q", env)
		require.ErrorContainsf(t, err, fmt.Sprintf("line %d: ", line), "%q", env)
	}
}

// Errors name the line, not an invalid key, which may be a line of a multi-line secret, and cut a long key short.
func TestValidateChangedEntriesDoesNotEchoKeys(t *testing.T) {
	err := validateChangedEntries("TLS_KEY=-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg+kqhkiG9w0BAQEFAASC=x", nil)
	require.ErrorContains(t, err, "line 2: ")
	require.NotContains(t, err.Error(), "MIIEvQ")

	longKey := strings.Repeat("A", 100)
	err = validateChangedEntries(longKey+"=hi\x1b[2J", nil)
	require.ErrorContains(t, err, "the value of "+strings.Repeat("A", 40)+"…")
	require.NotContains(t, err.Error(), strings.Repeat("A", 41))
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
	_, _, err = parsePostedEnv(strings.Repeat("A=1\n", maxPostedEnvLines))
	require.NoError(t, err, "a newline at the end doesn't start another line")
	_, _, err = parsePostedEnv(strings.Repeat("A=1\n", maxPostedEnvLines) + "B=2")
	require.ErrorIs(t, err, errBodyTooLarge)
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

// An app whose current env has an entry the gateway wouldn't accept today can still change its other keys: the CLI
// posts every entry back, and only new or changed ones are checked.
func TestReleaseCreateLeavesExistingEntriesAlone(t *testing.T) {
	f := newProtectedEnvFixture(t)
	f.rack.setEnv(protectedEnvRackEnv + "\nspring.profiles.active=prod\nBANNER=hi\x1b[1m")
	existing := maskedProtectedEnv + "\nspring.profiles.active=prod\nBANNER=hi\x1b[1m"

	req, _ := f.request(t, f.deployer, existing+"\nNEW_FLAG=1", "", "", stepUpAgeFresh)
	rr := f.proxy(req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	releases := f.rack.releases()
	require.Len(t, releases, 1)
	require.Contains(t, releases[0].env, "NEW_FLAG=1")
	require.Contains(t, releases[0].env, "spring.profiles.active=prod")

	changed := strings.Replace(existing, "spring.profiles.active=prod", "spring.profiles.active=dev", 1)
	req, _ = f.request(t, f.deployer, changed, "", "", stepUpAgeFresh)
	rr = f.proxy(req)
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "line 4: ")
	require.Len(t, f.rack.releases(), 1)
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
