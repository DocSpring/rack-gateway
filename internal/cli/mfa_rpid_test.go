package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRPID(t *testing.T) {
	cases := []struct {
		host, server, want string
		ok                 bool
	}{
		{"gateway-us.example.ts.net", "", "gateway-us.example.ts.net", true},
		{"gateway-us.example.ts.net", "gateway-us.example.ts.net", "gateway-us.example.ts.net", true},
		{"Gateway.Example.com", "gateway.example.com", "gateway.example.com", true},
		{"gateway.example.com", "example.com", "example.com", true},
		{"gateway.example.com", "google.com", "", false},
		{"gateway.example.com", "evil-example.com", "", false},
		{"gateway.example.com", "com", "", false},
		{"gateway-us.example.ts.net", "gateway-eu.example.ts.net", "", false},
		{"localhost", "localhost", "localhost", true},
	}
	for _, tc := range cases {
		got, err := resolveRPID(tc.host, tc.server)
		if !tc.ok {
			require.Errorf(t, err, "host=%s server=%s", tc.host, tc.server)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

func TestBuildAssertionOptionsUsesGatewayOriginAndRejectsForeignRPID(t *testing.T) {
	start := &webAuthnStartResponse{}
	start.Options.PublicKey.Challenge = "abc"
	start.Options.PublicKey.RPID = "gateway-us.example.ts.net"
	start.Options.PublicKey.UserVerification = "required"

	opts, err := buildAssertionOptions("https://gateway-us.example.ts.net/", []string{"cred"}, start)
	require.NoError(t, err)
	require.Equal(t, "gateway-us.example.ts.net", opts.RPID)
	require.Equal(t, "https://gateway-us.example.ts.net", opts.Origin)
	require.Equal(t, "required", opts.UserVerification)

	start.Options.PublicKey.RPID = "accounts.google.com"
	_, err = buildAssertionOptions("https://gateway-us.example.ts.net", []string{"cred"}, start)
	require.ErrorContains(t, err, "refusing")
}
