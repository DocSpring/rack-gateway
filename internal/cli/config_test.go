package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

// --api-token is the API token for every command, ahead of RACK_GATEWAY_API_TOKEN and the stored session, and with
// it RACK_GATEWAY_URL names the gateway. Convox commands skip the CLI's MFA prompt for it, as for the env var.
func TestAPITokenFlagIsUsedByEveryCommand(t *testing.T) {
	withLoggedInRack(t)
	previous := APITokenFlag
	t.Cleanup(func() { APITokenFlag = previous })
	envToken, flagToken := testAPIToken("env"), testAPIToken("flag")
	t.Setenv("RACK_GATEWAY_API_TOKEN", envToken)
	APITokenFlag = " " + flagToken + " "

	gatewayURL, token, err := LoadRackAuth("us")
	require.NoError(t, err)
	require.Equal(t, configTestGatewayURL, gatewayURL)
	require.Equal(t, flagToken, token)

	t.Setenv("RACK_GATEWAY_API_TOKEN", "")
	t.Setenv("RACK_GATEWAY_URL", "https://gateway-ci.example.com")
	gatewayURL, token, err = LoadRackAuth(rackFromEnvironment)
	require.NoError(t, err)
	require.Equal(t, "https://gateway-ci.example.com", gatewayURL)
	require.Equal(t, flagToken, token)

	mfaAuth, err := checkMFAAndGetAuth(&cobra.Command{}, "apps delete")
	require.NoError(t, err)
	require.Empty(t, mfaAuth)

	status, err := ResolveRackStatus(time.Now())
	require.NoError(t, err)
	require.Equal(t, "https://gateway-ci.example.com", status.GatewayURL)
	require.Empty(t, status.StatusLines)
}

// testAPIToken is a made-up API token.
func testAPIToken(name string) string {
	return "rgw_" + name
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
// partial file, and no temporary file is left behind. A reader that opened the old file still reads all of it.
func TestSaveConfigReplacesTheFileAtomically(t *testing.T) {
	withLoggedInRack(t)
	reader, err := os.Open(filepath.Clean(configFile()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	require.NoError(t, SetCurrentRack("eu"))

	var old Config
	require.NoError(t, json.NewDecoder(reader).Decode(&old), "the old file was rewritten in place")
	require.Equal(t, "us", old.Current)
	cfg, _, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "eu", cfg.Current)

	entries, err := os.ReadDir(ConfigPath)
	require.NoError(t, err)
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	require.Equal(t, []string{"config.json", "config.lock"}, names, "no temporary file is left")
}

// RACK_GATEWAY_URL can name the gateway of a rack that has none yet, but it can't repoint a configured rack:
// `rack-gateway login <rack> <url>` does that.
func TestLoginGatewayURLNeverRepointsAConfiguredRack(t *testing.T) {
	withLoggedInRack(t)
	t.Setenv("RACK_GATEWAY_URL", "https://gateway.attacker.example")

	_, err := resolveLoginGatewayURL("us")
	require.ErrorContains(t, err, "RACK_GATEWAY_URL (https://gateway.attacker.example) is not the gateway of rack us")
	configured, err := LoadGatewayURL("us")
	require.NoError(t, err)
	require.Equal(t, configTestGatewayURL, configured, "the rack's gateway is unchanged")

	t.Setenv("RACK_GATEWAY_URL", "rack-gateway-us.example.com/")
	gatewayURL, err := resolveLoginGatewayURL("us")
	require.NoError(t, err)
	require.Equal(t, configTestGatewayURL, gatewayURL)

	t.Setenv("RACK_GATEWAY_URL", "https://gateway-eu.example.com")
	gatewayURL, err = resolveLoginGatewayURL("eu")
	require.NoError(t, err)
	require.Equal(t, "https://gateway-eu.example.com", gatewayURL, "a new rack takes RACK_GATEWAY_URL")
	_, err = LoadGatewayURL("eu")
	require.Error(t, err, "the rack is only saved once the login succeeds")
}

// A login to the rack's own gateway keeps its settings; a login to another gateway starts the rack afresh.
func TestSaveLoginKeepsSettingsForTheSameGateway(t *testing.T) {
	withCLIConfig(t, Config{Current: "eu", Gateways: map[string]GatewayConfig{
		"us": {URL: configTestGatewayURL + "/", Token: "old-session", MFAPreference: "totp"},
	}})
	login := &LoginResponse{Token: "new-session", Email: "dev@example.com", ExpiresAt: time.Now().Add(time.Hour)}

	require.NoError(t, SaveLogin("us", configTestGatewayURL, login))
	cfg, _, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "us", cfg.Current)
	require.Equal(t, "new-session", cfg.Gateways["us"].Token)
	require.Equal(t, "totp", cfg.Gateways["us"].MFAPreference)

	require.NoError(t, SaveLogin("us", "https://gateway-new.example.com", login))
	cfg, _, err = LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://gateway-new.example.com", cfg.Gateways["us"].URL)
	require.Equal(t, "new-session", cfg.Gateways["us"].Token)
	require.Empty(t, cfg.Gateways["us"].MFAPreference)
}

// Commands that change the config at the same time each apply their change to the latest file, so none is lost.
func TestConcurrentConfigChangesAreAllKept(t *testing.T) {
	withLoggedInRack(t)
	const racks = 20
	var wg sync.WaitGroup
	for i := range racks {
		wg.Go(func() {
			login := &LoginResponse{Token: fmt.Sprintf("session-%d", i), ExpiresAt: time.Now().Add(time.Hour)}
			assert.NoError(t, SaveLogin(fmt.Sprintf("rack-%d", i), configTestGatewayURL, login))
		})
	}
	wg.Wait()

	cfg, _, err := LoadConfig()
	require.NoError(t, err)
	require.Len(t, cfg.Gateways, racks+1)
	for i := range racks {
		require.Equal(t, fmt.Sprintf("session-%d", i), cfg.Gateways[fmt.Sprintf("rack-%d", i)].Token)
	}
}
