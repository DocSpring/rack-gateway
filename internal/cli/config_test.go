package cli

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const configTestGatewayURL = "https://rack-gateway-us.example.com"

func withLoggedInRack(t *testing.T) {
	t.Helper()
	withCLIConfig(t, Config{Current: "us", Gateways: map[string]GatewayConfig{
		"us": {URL: configTestGatewayURL, Token: "session-token", ExpiresAt: time.Now().Add(time.Hour)},
	}})
}

// A stored session token is only sent to the gateway that issued it: RACK_GATEWAY_URL can't redirect it.
func TestLoadRackAuthNeverSendsTheSessionTokenElsewhere(t *testing.T) {
	withLoggedInRack(t)
	t.Setenv("RACK_GATEWAY_URL", "https://gateway.attacker.example")

	gatewayURL, token, err := LoadRackAuth("us")
	require.ErrorContains(t, err, "RACK_GATEWAY_URL (https://gateway.attacker.example) is not the gateway of rack us")
	require.Empty(t, gatewayURL)
	require.Empty(t, token)
}

func TestLoadRackAuthAcceptsTheRacksOwnURL(t *testing.T) {
	withLoggedInRack(t)
	t.Setenv("RACK_GATEWAY_URL", "rack-gateway-us.example.com/")

	gatewayURL, token, err := LoadRackAuth("us")
	require.NoError(t, err)
	require.Equal(t, configTestGatewayURL, gatewayURL)
	require.Equal(t, "session-token", token)
}

// With an API token (CI), RACK_GATEWAY_URL is the gateway and the rack name is only a label.
func TestLoadRackAuthUsesTheEnvGatewayURLWithAnAPIToken(t *testing.T) {
	withLoggedInRack(t)
	t.Setenv("RACK_GATEWAY_URL", "https://gateway-ci.example.com")
	t.Setenv("RACK_GATEWAY_API_TOKEN", "rgw_token")

	gatewayURL, token, err := LoadRackAuth("(from environment)")
	require.NoError(t, err)
	require.Equal(t, "https://gateway-ci.example.com", gatewayURL)
	require.Equal(t, "rgw_token", token)
}

// The config holds session tokens: saving it takes away any group or other permissions it was given.
func TestSaveConfigTightensLoosePermissions(t *testing.T) {
	withLoggedInRack(t)
	looseDir, looseFile := os.FileMode(0o755), os.FileMode(0o644)
	require.NoError(t, os.Chmod(ConfigPath, looseDir))
	require.NoError(t, os.Chmod(configFile(), looseFile))

	require.NoError(t, SetCurrentRack("us"))

	dirInfo, err := os.Stat(ConfigPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	fileInfo, err := os.Stat(configFile())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

// Saving replaces the file (a new file renamed over it) rather than rewriting it in place, so a reader never sees a
// partial file, and no temporary file is left behind.
func TestSaveConfigReplacesTheFileAtomically(t *testing.T) {
	withLoggedInRack(t)
	before, err := os.Stat(configFile())
	require.NoError(t, err)

	require.NoError(t, SetCurrentRack("eu"))

	after, err := os.Stat(configFile())
	require.NoError(t, err)
	require.False(t, os.SameFile(before, after), "the file was rewritten in place")
	cfg, _, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "eu", cfg.Current)

	entries, err := os.ReadDir(ConfigPath)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only config.json is left")
	require.Equal(t, "config.json", entries[0].Name())
}
