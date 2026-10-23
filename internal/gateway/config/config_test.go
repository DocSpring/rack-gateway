package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDevKeySuccess(t *testing.T) {
	key, err := generateDevKey()
	require.NoError(t, err)
	assert.Len(t, key, 44)
}

func TestGenerateDevKeyError(t *testing.T) {
	original := randRead
	defer func() { randRead = original }()

	randRead = func([]byte) (int, error) {
		return 0, errors.New("rng failure")
	}

	key, err := generateDevKey()
	assert.Error(t, err)
	assert.Empty(t, key)
}

func TestLoadDevModeGeneratesSecret(t *testing.T) {
	t.Setenv("DEV_MODE", "true")
	t.Setenv("APP_SECRET_KEY", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.SessionSecret)
}

func TestLoadDevModeGenerateKeyFailure(t *testing.T) {
	original := randRead
	defer func() { randRead = original }()

	randRead = func([]byte) (int, error) {
		return 0, errors.New("rng failure")
	}

	t.Setenv("DEV_MODE", "true")
	t.Setenv("APP_SECRET_KEY", "")

	cfg, err := Load()
	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate dev secret key")
}

func TestLoadProductionRequiresSecret(t *testing.T) {
	t.Setenv("DEV_MODE", "false")
	t.Setenv("APP_SECRET_KEY", "")

	cfg, err := Load()
	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "APP_SECRET_KEY is required in production")
}

// A whitespace-only APP_SECRET_KEY isn't a secret, in any mode.
func TestLoadRefusesBlankSecret(t *testing.T) {
	for _, devMode := range []string{"false", "true"} {
		t.Setenv("DEV_MODE", devMode)
		t.Setenv("APP_SECRET_KEY", " \n")

		cfg, err := Load()
		assert.Nil(t, cfg)
		require.Errorf(t, err, "DEV_MODE=%s", devMode)
		assert.Contains(t, err.Error(), "APP_SECRET_KEY is blank")
	}
}

func TestLoadProductionRequiresAllowedDomain(t *testing.T) {
	t.Setenv("DEV_MODE", "false")
	t.Setenv("APP_SECRET_KEY", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAIN", "")

	cfg, err := Load()
	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GOOGLE_ALLOWED_DOMAIN is required in production")
}

func TestLoadProductionRequiresDomain(t *testing.T) {
	t.Setenv("DEV_MODE", "false")
	t.Setenv("APP_SECRET_KEY", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAIN", "example.com")
	t.Setenv("DOMAIN", "")

	cfg, err := Load()
	assert.Nil(t, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DOMAIN is required in production")
}

// The gateway sends the rack's credential to RACK_HOST, so a release build only takes an https URL without
// credentials, and a bare host means https.
func TestParseRackURL(t *testing.T) {
	accepted := map[string]string{
		"api.rack.example:5443":          "https://api.rack.example:5443",
		"https://api.rack.example:5443/": "https://api.rack.example:5443",
		"https://api.rack.example":       "https://api.rack.example",
	}
	for raw, want := range accepted {
		got, err := parseRackURL(" "+raw+"\n", false)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	refused := map[string]string{
		"http://api.rack.example:5443":                 "must be an https:// URL",
		"ftp://api.rack.example":                       "must be an https:// URL, not ftp://",
		"https://convox:pw@api.rack.example":           "can't include credentials",
		"https://api.rack.example/apps":                "no path or query",
		"https://api.rack.example?q=x":                 "no path or query",
		"https://":                                     "no path or query",
		"https://api.rack.example:5443/%zz":            "isn't a valid URL",
		"https://api.rack.example:5443#fragment":       "no path or query",
		"https://user@api.rack.example:5443/somewhere": "can't include credentials",
	}
	for raw, want := range refused {
		_, err := parseRackURL(raw, false)
		require.Error(t, err, raw)
		assert.Contains(t, err.Error(), want, raw)
	}

	// The mock rack in development and tests serves plain http.
	got, err := parseRackURL("http://mock-convox:5443", true)
	require.NoError(t, err)
	assert.Equal(t, "http://mock-convox:5443", got)
}

func TestPublicURL(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		devMode bool
		want    string
	}{
		{name: "production domain", domain: "gateway.example.com", want: "https://gateway.example.com"},
		{name: "dev localhost", domain: "localhost", devMode: true, want: "http://localhost:8447"},
		{name: "dev localhost:port", domain: "localhost:9999", devMode: true, want: "http://localhost:8447"},
		{name: "localhost:port outside dev mode", domain: "localhost:9447", want: "http://localhost:9447"},
		{name: "bare localhost outside dev mode", domain: "localhost", want: "http://localhost:8447"},
		{name: "no domain in dev mode", domain: "", devMode: true, want: "http://localhost:8447"},
		{name: "no domain outside dev mode", domain: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Domain: tt.domain, DevMode: tt.devMode, Port: "8447"}
			assert.Equal(t, tt.want, cfg.PublicURL())
		})
	}
}
