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
		{name: "localhost outside dev mode", domain: "localhost:9447", want: "http://localhost:9447"},
		{name: "no domain in dev mode", domain: "", devMode: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Domain: tt.domain, DevMode: tt.devMode, Port: "8447"}
			assert.Equal(t, tt.want, cfg.PublicURL())
		})
	}
}
