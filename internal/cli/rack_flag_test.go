package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// parseRootFlags parses args with the real root command's global flags, as the CLI does before running a command.
func parseRootFlags(t *testing.T, args ...string) {
	t.Helper()
	require.NoError(t, newRootCommand().PersistentFlags().Parse(args))
}

func withRackConfig(t *testing.T) {
	t.Helper()
	withCLIConfig(t, Config{Gateways: map[string]GatewayConfig{
		"staging": {URL: "https://rack-gateway-staging.example.com"},
		"us":      {URL: "https://rack-gateway-us.example.com"},
		"eu":      {URL: "https://rack-gateway-eu.example.com"},
	}})
}

// A repeated --rack used to keep only its last value, so `approve --rack us --rack eu` approved EU alone, with no
// warning. Repeating the flag and listing racks with commas must select the same racks.
func TestResolveRacksTakesRepeatedAndCommaSeparatedRackFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"repeated", []string{"--rack", "us", "--rack", "eu"}, []string{"us", "eu"}},
		{"short and long", []string{"-r", "us", "--rack=eu"}, []string{"us", "eu"}},
		{"comma-separated", []string{"--rack", "us,eu"}, []string{"us", "eu"}},
		{"mixed", []string{"--rack", "staging", "--rack", "us,eu"}, []string{"staging", "us", "eu"}},
		{"order given", []string{"--rack", "eu", "-r", "staging,us"}, []string{"eu", "staging", "us"}},
		{"duplicates", []string{"--rack", "us", "--rack", "us, eu", "-r", "eu"}, []string{"us", "eu"}},
		{"blank values", []string{"--rack", "us", "--rack", "", "--rack", " ,eu,"}, []string{"us", "eu"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRackConfig(t)
			parseRootFlags(t, tc.args...)

			racks, err := resolveRacks()

			require.NoError(t, err)
			require.Equal(t, tc.want, racks)
		})
	}
}

func TestResolveRacksTakesRepeatedAll(t *testing.T) {
	withRackConfig(t)
	parseRootFlags(t, "--rack", "all", "--rack", "all")

	racks, err := resolveRacks()

	require.NoError(t, err)
	require.ElementsMatch(t, []string{"staging", "us", "eu"}, racks)
}

func TestResolveRacksRefusesAllWithOtherRacks(t *testing.T) {
	for _, args := range [][]string{
		{"--rack", "all", "--rack", "us"},
		{"--rack", "us", "--rack", "all"},
		{"--rack", "all,eu"},
	} {
		withRackConfig(t)
		parseRootFlags(t, args...)

		_, err := resolveRacks()

		require.ErrorIs(t, err, errAllWithOtherRacks, args)
	}
}

// Commands that use one rack must refuse several rather than quietly use one of them.
func TestSelectedRackRefusesSeveralRacks(t *testing.T) {
	for _, args := range [][]string{
		{"--rack", "us", "--rack", "eu"},
		{"--rack", "us,eu"},
		{"--rack", "us", "-r", "us,eu"},
	} {
		withRackConfig(t)
		parseRootFlags(t, args...)

		_, err := SelectedRack()

		require.EqualError(t, err, "this command uses one rack, but several were selected: us, eu", args)
	}
}

func TestSelectedRackRefusesSeveralRacksFromTheEnvironment(t *testing.T) {
	withRackConfig(t)
	t.Setenv("RACK_GATEWAY_RACK", "us,eu")

	_, err := SelectedRack()

	require.EqualError(t, err, "this command uses one rack, but several were selected: us, eu")
}

func TestSelectedRackTakesOneRackNamedMoreThanOnce(t *testing.T) {
	for _, args := range [][]string{
		{"--rack", "us"},
		{"--rack", "us", "--rack", "us"},
		{"--rack", "us,us"},
		{"--rack", "us", "--rack", ""},
	} {
		withRackConfig(t)
		parseRootFlags(t, args...)

		rack, err := SelectedRack()

		require.NoError(t, err, args)
		require.Equal(t, "us", rack, args)
	}
}

// --rack couldn't select a rack whose name has a comma, so login refuses one.
func TestLoginRefusesRackNameWithComma(t *testing.T) {
	withRackConfig(t)

	for _, args := range [][]string{{"us,eu"}, {"us,eu", "https://rack-gateway-us.example.com"}} {
		_, _, err := resolveLoginTarget(args)

		require.EqualError(t, err, `rack name "us,eu" can't contain a comma: --rack uses commas to separate racks`)
	}
}
