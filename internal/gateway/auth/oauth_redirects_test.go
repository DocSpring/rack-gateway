package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The identity provider always sends the browser back to the gateway's own callback, which completes the login.
// The SPA has no callback page: a /app/auth/callback redirect URI is treated as the gateway's base URL.
func TestDeriveRedirectsUseTheGatewayCallbacks(t *testing.T) {
	for _, input := range []string{
		"https://gateway.example.com",
		"https://gateway.example.com/",
		"https://gateway.example.com/api/v1/auth/web/callback",
		"https://gateway.example.com/app/auth/callback",
	} {
		base, web, cli, err := deriveRedirects(input)
		require.NoError(t, err, input)
		require.Equal(t, "https://gateway.example.com", base, input)
		require.Equal(t, "https://gateway.example.com/api/v1/auth/web/callback", web, input)
		require.Equal(t, "https://gateway.example.com/api/v1/auth/cli/callback", cli, input)
	}
}
