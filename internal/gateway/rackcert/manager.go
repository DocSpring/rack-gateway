package rackcert

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// pinRecheckInterval is how long TLSConfig reuses its configuration before it reads the pinned certificate from the
// database again, so a refresh made by another gateway instance takes effect on this one within it.
const pinRecheckInterval = 30 * time.Second

// ErrFingerprintMismatch is returned when the rack presents a certificate with an unexpected fingerprint.
var ErrFingerprintMismatch = fmt.Errorf("rack TLS certificate fingerprint mismatch")

// FingerprintMismatchError provides details about a certificate mismatch.
type FingerprintMismatchError struct {
	Expected string
	Actual   string
}

// Error implements the error interface.
func (e *FingerprintMismatchError) Error() string {
	return fmt.Sprintf(
		"rack TLS certificate fingerprint mismatch (expected %s, got %s)",
		strings.ToUpper(e.Expected),
		strings.ToUpper(e.Actual),
	)
}

// Unwrap allows errors.Is/As to match ErrFingerprintMismatch.
func (_ *FingerprintMismatchError) Unwrap() error {
	return ErrFingerprintMismatch
}

// Manager handles pinning and verifying the rack TLS certificate.
type Manager struct {
	cfg *config.Config
	db  *db.Database

	mu      sync.RWMutex
	cert    *db.RackTLSCert
	tlsConf *tls.Config
	// checkedAt is when cert was last read from (or written to) the database.
	checkedAt time.Time
}

// NewManager constructs a new rack certificate manager.
func NewManager(cfg *config.Config, database *db.Database) *Manager {
	return &Manager{cfg: cfg, db: database}
}

// CurrentCertificate returns the currently stored certificate if available, read again from the database every
// pinRecheckInterval like TLSConfig's.
func (m *Manager) CurrentCertificate(context.Context) (*db.RackTLSCert, bool, error) {
	if m.recentTLSConfig() == nil {
		if err := m.loadFromDB(); err != nil && m.currentTLSConfig() == nil {
			return nil, false, err
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, false, nil
	}
	certCopy := *m.cert
	return &certCopy, true, nil
}

// TLSConfig returns a TLS configuration that validates the pinned certificate, fetching it if necessary.
//
// Every caller gets the same *tls.Config until the pinned certificate changes, so httpclient can keep one pool of
// connections for it (a new pointer tells it the certificate changed). Callers must not modify it; net/http and
// gorilla/websocket clone it before each handshake. The pin is read from the database again every
// pinRecheckInterval, as another instance may have refreshed it.
func (m *Manager) TLSConfig(ctx context.Context) (*tls.Config, error) {
	rck, ok := m.rackConfig()
	if !ok {
		return nil, fmt.Errorf("rack TLS manager: no rack configured")
	}
	if !strings.HasPrefix(strings.ToLower(rck.URL), "https://") {
		return nil, nil
	}

	if cfg := m.recentTLSConfig(); cfg != nil {
		return cfg, nil
	}

	if err := m.loadFromDB(); err != nil {
		if cfg := m.currentTLSConfig(); cfg != nil {
			// The pin in use fails closed: a rack certificate that changed meanwhile is refused.
			gtwlog.Errorf("rack TLS manager: can't re-read the pinned certificate, keeping the current one: %v", err)
			return cfg, nil
		}
		return nil, err
	}

	if cfg := m.currentTLSConfig(); cfg != nil {
		return cfg, nil
	}

	return m.fetchAndStore(ctx, rck, nil)
}

func (m *Manager) currentTLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tlsConf
}

// recentTLSConfig returns the TLS configuration when its pin was read less than pinRecheckInterval ago.
func (m *Manager) recentTLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if time.Since(m.checkedAt) >= pinRecheckInterval {
		return nil
	}
	return m.tlsConf
}

// Refresh forces a re-fetch of the rack certificate, replacing the stored value.
func (m *Manager) Refresh(ctx context.Context, updatedBy *int64) (*db.RackTLSCert, error) {
	rck, ok := m.rackConfig()
	if !ok {
		return nil, fmt.Errorf("rack TLS manager: no rack configured")
	}
	if !strings.HasPrefix(strings.ToLower(rck.URL), "https://") {
		return nil, fmt.Errorf("rack TLS manager: rack URL is not https, cannot refresh certificate")
	}

	if _, err := m.fetchAndStore(ctx, rck, updatedBy); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, fmt.Errorf("rack TLS manager: certificate not cached after refresh")
	}
	certCopy := *m.cert
	return &certCopy, nil
}

func (m *Manager) loadFromDB() error {
	if m.db == nil {
		return fmt.Errorf("rack TLS manager: database unavailable")
	}
	cert, ok, err := m.db.GetRackTLSCert()
	if err != nil {
		return err
	}
	if !ok || m.samePin(cert) {
		m.mu.Lock()
		m.checkedAt = time.Now()
		m.mu.Unlock()
		return nil
	}
	rck, ok := m.rackConfig()
	if !ok {
		return fmt.Errorf("rack TLS manager: no rack configured")
	}
	if !strings.HasPrefix(strings.ToLower(rck.URL), "https://") {
		return nil
	}
	host, err := rackHostname(rck)
	if err != nil {
		return err
	}
	cfg, err := buildTLSConfig(cert, host)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cert = cert
	m.tlsConf = cfg
	m.checkedAt = time.Now()
	m.mu.Unlock()
	return nil
}

// samePin reports whether cert is the certificate the current TLS configuration pins, which then stays in use.
func (m *Manager) samePin(cert *db.RackTLSCert) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tlsConf != nil && m.cert != nil && strings.EqualFold(m.cert.Fingerprint, cert.Fingerprint)
}

func (m *Manager) fetchAndStore(ctx context.Context, rck config.RackConfig, updatedBy *int64) (*tls.Config, error) {
	cert, err := fetchRackCertificate(ctx, rck)
	if err != nil {
		return nil, err
	}
	host, err := rackHostname(rck)
	if err != nil {
		return nil, err
	}
	cfg, err := buildTLSConfig(cert, host)
	if err != nil {
		return nil, err
	}
	if m.db != nil {
		if err := m.db.UpsertRackTLSCert(cert, updatedBy); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	m.cert = cert
	m.tlsConf = cfg
	m.checkedAt = time.Now()
	m.mu.Unlock()
	return cfg, nil
}

func (m *Manager) rackConfig() (config.RackConfig, bool) {
	if m.cfg == nil {
		return config.RackConfig{}, false
	}
	if r, ok := m.cfg.Racks["default"]; ok && r.Enabled {
		return r, true
	}
	if r, ok := m.cfg.Racks["local"]; ok && r.Enabled {
		return r, true
	}
	return config.RackConfig{}, false
}

func fetchRackCertificate(ctx context.Context, rck config.RackConfig) (*db.RackTLSCert, error) {
	parsed, err := parseRackURL(rck.URL)
	if err != nil {
		return nil, err
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("rack TLS manager: missing hostname in rack URL")
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(host, port)
	if ctx == nil {
		ctx = context.Background()
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// #nosec G402 -- this fetch reads the rack's self-signed certificate so its fingerprint can be pinned; no
		// request is sent over the connection.
		Config: &tls.Config{InsecureSkipVerify: true, ServerName: host},
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("rack TLS manager: failed to dial rack: %w", err)
	}
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		if cerr := conn.Close(); cerr != nil {
			return nil, fmt.Errorf("rack TLS manager: expected TLS connection and failed close: %w", cerr)
		}
		return nil, fmt.Errorf("rack TLS manager: expected TLS connection, got %T", conn)
	}
	defer func() { _ = tlsConn.Close() }()
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("rack TLS manager: rack presented no certificate")
	}
	leaf := state.PeerCertificates[0]
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	if pemBytes == nil {
		return nil, fmt.Errorf("rack TLS manager: failed to encode certificate")
	}
	fp := sha256.Sum256(leaf.Raw)
	return &db.RackTLSCert{
		PEM:         string(pemBytes),
		Fingerprint: strings.ToUpper(hex.EncodeToString(fp[:])),
		FetchedAt:   time.Now().UTC(),
	}, nil
}

func buildTLSConfig(cert *db.RackTLSCert, host string) (*tls.Config, error) {
	decoded, err := hex.DecodeString(strings.ReplaceAll(cert.Fingerprint, ":", ""))
	if err != nil {
		return nil, fmt.Errorf("rack TLS manager: invalid fingerprint encoding: %w", err)
	}
	expected := make([]byte, len(decoded))
	copy(expected, decoded)

	// The rack's certificate is self-signed, so the standard chain verification is off and VerifyConnection checks
	// the pinned fingerprint instead. VerifyConnection runs on every handshake, resumed sessions included
	// (VerifyPeerCertificate doesn't run on a resumed one).
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
		// #nosec G402 -- VerifyConnection pins the rack's certificate.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("rack TLS manager: rack presented no certificate")
			}
			sum := sha256.Sum256(state.PeerCertificates[0].Raw)
			if !equalBytes(sum[:], expected) {
				return &FingerprintMismatchError{
					Expected: cert.Fingerprint,
					Actual:   strings.ToUpper(hex.EncodeToString(sum[:])),
				}
			}
			return nil
		},
	}, nil
}

func rackHostname(rck config.RackConfig) (string, error) {
	parsed, err := parseRackURL(rck.URL)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("rack TLS manager: missing hostname in rack URL")
	}
	return host, nil
}

func parseRackURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("rack TLS manager: rack URL is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("rack TLS manager: invalid rack URL: %w", err)
	}
	return parsed, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AsFingerprintMismatch attempts to extract a fingerprint mismatch error.
func AsFingerprintMismatch(err error) (*FingerprintMismatchError, bool) {
	if err == nil {
		return nil, false
	}
	var mismatch *FingerprintMismatchError
	if errors.As(err, &mismatch) {
		return mismatch, true
	}
	return nil, false
}
