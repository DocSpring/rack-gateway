package cli

import (
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLoginTargetUsesCurrentRackURLWhenTokenExpired(t *testing.T) {
	withCLIConfig(t, Config{
		Current: "us",
		Gateways: map[string]GatewayConfig{
			"us": {
				URL:       "https://rack-gateway-us.example.com",
				Token:     "expired-token",
				ExpiresAt: time.Now().Add(-time.Hour),
			},
		},
	})

	rack, gatewayURL, err := resolveLoginTarget(nil)

	require.NoError(t, err)
	assert.Equal(t, "us", rack)
	assert.Equal(t, "https://rack-gateway-us.example.com", gatewayURL)
}

func TestResolveLoginTargetUsesNamedRackURL(t *testing.T) {
	withCLIConfig(t, Config{
		Gateways: map[string]GatewayConfig{
			"us": {
				URL:       "https://rack-gateway-us.example.com",
				Token:     "expired-token",
				ExpiresAt: time.Now().Add(-time.Hour),
			},
		},
	})

	rack, gatewayURL, err := resolveLoginTarget([]string{"us"})

	require.NoError(t, err)
	assert.Equal(t, "us", rack)
	assert.Equal(t, "https://rack-gateway-us.example.com", gatewayURL)
}

func TestResolveRacksSplitsGlobalRackFlag(t *testing.T) {
	withCLIConfig(t, Config{
		Gateways: map[string]GatewayConfig{
			"us": {URL: "https://rack-gateway-us.example.com"},
			"eu": {URL: "https://rack-gateway-eu.example.com"},
			"au": {URL: "https://rack-gateway-au.example.com"},
		},
	})
	RackFlag = "us, eu,au"

	racks, err := resolveRacks()

	require.NoError(t, err)
	assert.Equal(t, []string{"us", "eu", "au"}, racks)
}

func TestResolveRacksRejectsUnknownRack(t *testing.T) {
	withCLIConfig(t, Config{
		Gateways: map[string]GatewayConfig{
			"us": {URL: "https://rack-gateway-us.example.com"},
		},
	})
	RackFlag = "unknown"

	_, err := resolveRacks()

	require.Error(t, err)
	assert.Equal(t, `No rack found with name "unknown"`, err.Error())
}

func TestApproveBySearchReturnsExpiredToken(t *testing.T) {
	withCLIConfig(t, Config{
		Gateways: map[string]GatewayConfig{
			"us": {
				URL:       "https://rack-gateway-us.example.com",
				Token:     "expired-token",
				ExpiresAt: time.Now().Add(-time.Hour),
			},
		},
	})

	err := approveBySearch(&cobra.Command{}, []string{"us"}, "docspring", "", "13b02be", "")

	require.ErrorIs(t, err, ErrTokenExpired)
	assert.Equal(t, "token expired", err.Error())
}

func TestGatewayResponseErrorMapsExpiredSessionToTokenExpired(t *testing.T) {
	err := gatewayResponseError(http.StatusUnauthorized, []byte("authentication failed: session expired\n"))

	require.ErrorIs(t, err, ErrTokenExpired)
	assert.Equal(t, "token expired", err.Error())
}

func withCLIConfig(t *testing.T, cfg Config) {
	t.Helper()

	previousConfigPath := ConfigPath
	previousRackFlag := RackFlag
	previousAPITokenFlag := APITokenFlag
	t.Cleanup(func() {
		ConfigPath = previousConfigPath
		RackFlag = previousRackFlag
		APITokenFlag = previousAPITokenFlag
	})

	ConfigPath = t.TempDir()
	RackFlag = ""
	APITokenFlag = ""
	t.Setenv("RACK_GATEWAY_URL", "")
	t.Setenv("RACK_GATEWAY_RACK", "")
	t.Setenv("RACK_GATEWAY_API_TOKEN", "")

	require.NoError(t, SaveConfig(&cfg))
}
