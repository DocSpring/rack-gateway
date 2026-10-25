package cmdenv

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestHeadersRoundTrip(t *testing.T) {
	for _, r := range []Request{
		{},
		{Env: []string{"STRIPE_KEY", "http_proxy"}},
		{Env: []string{"STRIPE_KEY"}, Write: true},
		{AllEnvVars: true, Write: true},
	} {
		header := http.Header{}
		for name, value := range r.Headers() {
			header.Set(name, value)
		}
		read, err := RequestFrom(header)
		require.NoError(t, err)
		require.Equal(t, r, read)
		require.Equal(t, r.IsZero(), len(header) == 0)
	}

	read, err := RequestFrom(http.Header{EnvHeader: {" A , B "}})
	require.NoError(t, err)
	require.Equal(t, Request{Env: []string{"A", "B"}}, read)
}

func TestRequestFromRefusesMalformedHeaders(t *testing.T) {
	for want, header := range map[string]http.Header{
		"gives X-Command-Env more than once": {EnvHeader: {"A", "B"}},
		"X-Command-Write must be true":       {WriteHeader: {"yes"}},
		"X-Command-All-Env-Vars must be":     {AllEnvVarsHeader: {"1"}},
		"isn't an env var name":              {EnvHeader: {"A,B-C"}},
		`"" isn't an env var name`:           {EnvHeader: {"A,,B"}},
		"no command can have RACK_URL":       {EnvHeader: {"RACK_URL"}},
		"can't name some with --env too":     {EnvHeader: {"A"}, AllEnvVarsHeader: {"true"}},
	} {
		_, err := RequestFrom(header)
		require.ErrorContains(t, err, want)
	}
}
