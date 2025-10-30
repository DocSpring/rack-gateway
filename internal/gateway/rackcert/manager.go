package rackcert

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
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
)

// ErrFingerprintMismatch is returned when the rack presents a certificate with an unexpected fingerprint.
var ErrFingerprintMismatch = fmt.Errorf("rack TLS certificate fingerprint mismatch")

// FingerprintMismatchError provides details about a certificate mismatch.
type FingerprintMismatchError struct {
	Expected string
	Actual   string
}

// Error implements the error interface.
func (e *FingerprintMismatchError) Error() string {
	return fmt.Sprintf("rack TLS certificate fingerprint mismatch (expected %s, got %s)", strings.ToUpper(e.Expected), strings.ToUpper(e.Actual))
}

// Unwrap allows errors.Is/As to match ErrFingerprintMismatch.
func (e *FingerprintMismatchError) Unwrap() error {
	return ErrFingerprintMismatch
}

// Manager handles pinning and verifying the rack TLS certificate.
type Manager struct {
	cfg *config.Config
	db  *db.Database

	mu      sync.RWMutex
	cert    *db.RackTLSCert
	tlsConf *tls.Config
}

// NewManager constructs a new rack certificate manager.
func NewManager(cfg *config.Config, database *db.Database) *Manager {
	return &Manager{cfg: cfg, db: database}
}

// CurrentCertificate returns the currently stored certificate if available.
func (m *Manager) CurrentCertificate(ctx context.Context) (*db.RackTLSCert, bool, error) {
	m.mu.RLock()
	if m.cert != nil {
		copy := *m.cert
		m.mu.RUnlock()
		return &copy, true, nil
	}
	m.mu.RUnlock()

	if err := m.loadFromDB(); err != nil {
		return nil, false, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, false, nil
	}
	copy := *m.cert
	return &copy, true, nil
}

// TLSConfig returns a TLS configuration that validates the pinned certificate, fetching it if necessary.
func (m *Manager) TLSConfig(ctx context.Context) (*tls.Config, error) {
	rck, ok := m.rackConfig()
	if !ok {
		return nil, fmt.Errorf("rack TLS manager: no rack configured")
	}
	if !strings.HasPrefix(strings.ToLower(rck.URL), "https://") {
		return nil, nil
	}

	m.mu.RLock()
	if m.tlsConf != nil {
		clone := m.tlsConf.Clone()
		m.mu.RUnlock()
		return clone, nil
	}
	m.mu.RUnlock()

	if err := m.loadFromDB(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	if m.tlsConf != nil {
		clone := m.tlsConf.Clone()
		m.mu.RUnlock()
		return clone, nil
	}
	m.mu.RUnlock()

	cfg, err := m.fetchAndStore(ctx, rck, nil)
	if err != nil {
		return nil, err
	}
	return cfg.Clone(), nil
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
	copy := *m.cert
	return &copy, nil
}

func (m *Manager) loadFromDB() error {
	if m.db == nil {
		return fmt.Errorf("rack TLS manager: database unavailable")
	}
	cert, ok, err := m.db.GetRackTLSCert()
	if err != nil {
		return err
	}
	if !ok {
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
	m.mu.Unlock()
	return nil
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
		Config:    &tls.Config{InsecureSkipVerify: true, ServerName: host},
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
	defer tlsConn.Close() //nolint:errcheck // best-effort TLS close
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

	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("rack TLS manager: rack presented no certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
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
