package rackcert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// httpclient pools rack connections per *tls.Config, so the pinned config must stay the same object until the
// pinned certificate changes, and a refresh must produce a new one.
func TestTLSConfigIsSharedUntilTheCertificateIsRefreshed(t *testing.T) {
	rack := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rack.Close)

	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {Name: "default", URL: rack.URL, Enabled: true},
	}}
	mgr := NewManager(cfg, nil)
	ctx := context.Background()

	cert, err := mgr.Refresh(ctx, nil)
	require.NoError(t, err)
	require.NotEmpty(t, cert.Fingerprint)
	first, err := mgr.TLSConfig(ctx)
	require.NoError(t, err)
	second, err := mgr.TLSConfig(ctx)
	require.NoError(t, err)
	require.Same(t, first, second)

	_, err = mgr.Refresh(ctx, nil)
	require.NoError(t, err)
	refreshed, err := mgr.TLSConfig(ctx)
	require.NoError(t, err)
	require.NotSame(t, first, refreshed)
}
