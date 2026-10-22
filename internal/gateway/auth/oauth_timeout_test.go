package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The code exchange stops when the callback's request ends, so a provider that accepts the request and stalls
// can't hold the handler.
func TestCompleteLoginStopsWithTheRequest(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	provider := fakeOIDCProvider(t, key)
	mux, ok := provider.Config.Handler.(*http.ServeMux)
	require.True(t, ok)
	// The stalled token request ends when the test does. Cleanups run last first, so this runs before the
	// provider's server waits for its requests to finish.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	mux.HandleFunc("/token", func(http.ResponseWriter, *http.Request) { <-release })
	handler, err := NewOAuthHandler("client-id", "secret", "http://localhost:8447", "example.com", provider.URL)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = handler.CompleteLogin(ctx, "code", "", "")
	require.ErrorContains(t, err, "failed to exchange code for token")
	require.Less(t, time.Since(started), 5*time.Second)
}
