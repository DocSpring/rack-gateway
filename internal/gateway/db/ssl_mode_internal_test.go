package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// PGSSLMODE can make a DSN's sslmode stricter, never weaker.
func TestEnsureSSLModeNeverWeakensTheDSN(t *testing.T) {
	cases := map[string]struct{ dsn, pgsslmode, want string }{
		"raises require": {
			"postgres://u@db.example.com/gw?sslmode=require", "verify-full",
			"postgres://u@db.example.com/gw?sslmode=verify-full",
		},
		"keeps verify-full": {
			"postgres://u@db.example.com/gw?sslmode=verify-full", "require",
			"postgres://u@db.example.com/gw?sslmode=verify-full",
		},
		"sets a missing mode": {
			"postgres://u@db.example.com/gw", "require", "postgres://u@db.example.com/gw?sslmode=require",
		},
		"keeps the DSN without PGSSLMODE": {
			"postgres://u@db.example.com/gw?sslmode=disable", "", "postgres://u@db.example.com/gw?sslmode=disable",
		},
		"raises a keyword DSN":         {"host=db sslmode=require", "verify-full", "host=db sslmode=verify-full"},
		"keeps a stricter keyword DSN": {"host=db sslmode=verify-full", "disable", "host=db sslmode=verify-full"},
		"appends to a keyword DSN":     {"host=db dbname=gw", "require", "host=db dbname=gw sslmode=require"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PGSSLMODE", tc.pgsslmode)
			require.Equal(t, tc.want, ensureSSLMode(tc.dsn))
		})
	}
}
