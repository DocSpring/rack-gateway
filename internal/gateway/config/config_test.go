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
