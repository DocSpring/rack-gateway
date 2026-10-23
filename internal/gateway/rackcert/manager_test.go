package rackcert

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func newTLSRack(t *testing.T) *httptest.Server {
	t.Helper()
	rack := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rack.Close)
	return rack
}

func rackConfig(url string) *config.Config {
	return &config.Config{Racks: map[string]config.RackConfig{
		"default": {Name: "default", URL: url, Enabled: true},
	}}
}

// httpclient pools rack connections per *tls.Config, so the pinned config must stay the same object until the
// pinned certificate changes, and a refresh must produce a new one.
func TestTLSConfigIsSharedUntilTheCertificateIsRefreshed(t *testing.T) {
	rack := newTLSRack(t)
	mgr := NewManager(rackConfig(rack.URL), nil)
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

// A refresh on one gateway instance reaches the others within pinRecheckInterval, through the database. Until the
// pin changes, each instance keeps its TLS configuration (and connection pool).
func TestTLSConfigFollowsARefreshByAnotherInstance(t *testing.T) {
	database := dbtest.NewDatabase(t)
	rack := newTLSRack(t)
	ctx := context.Background()
	refresher := NewManager(rackConfig(rack.URL), database)
	other := NewManager(rackConfig(rack.URL), database)

	_, err := refresher.Refresh(ctx, nil)
	require.NoError(t, err)
	first, err := other.TLSConfig(ctx)
	require.NoError(t, err)
	other.checkedAt = time.Time{}
	unchanged, err := other.TLSConfig(ctx)
	require.NoError(t, err)
	require.Same(t, first, unchanged, "the same pin keeps its configuration")

	// Another instance pins a different certificate. Every httptest server presents the same test certificate, so
	// the new pin is stored directly.
	newCert := &db.RackTLSCert{
		PEM: "replacement", Fingerprint: strings.Repeat("AB", sha256.Size), FetchedAt: time.Now().UTC(),
	}
	require.NoError(t, database.UpsertRackTLSCert(newCert, nil))

	cached, err := other.TLSConfig(ctx)
	require.NoError(t, err)
	require.Same(t, first, cached, "the pin is re-read only after pinRecheckInterval")
	other.checkedAt = time.Now().Add(-pinRecheckInterval)
	updated, err := other.TLSConfig(ctx)
	require.NoError(t, err)
	require.NotSame(t, first, updated)
	current, ok, err := other.CurrentCertificate(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, newCert.Fingerprint, current.Fingerprint)
}
