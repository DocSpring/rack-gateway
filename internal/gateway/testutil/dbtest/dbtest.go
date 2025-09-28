package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
)

// Reset truncates all application tables to provide a clean slate for tests.
// Uses TRUNCATE ... CASCADE so FK references are handled.
func Reset(t *testing.T, database *db.Database) {
	t.Helper()
	_, err := database.DB().Exec(`
        TRUNCATE TABLE
          api_tokens,
          audit_logs,
          cli_login_states,
          users
        RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("failed to reset database: %v", err)
	}
}

// NewDatabase creates a unique temporary Postgres database for a test, runs migrations,
// and returns a connected *db.Database. The database is dropped on test cleanup.
func NewDatabase(t *testing.T) *db.Database {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		// Build from PG* env
		host := getenv("PGHOST", "localhost")
		port := getenv("PGPORT", "5432")
		user := getenv("PGUSER", getenv("USER", "postgres"))
		dbname := getenv("PGDATABASE", user)
		ssl := getenv("PGSSLMODE", "disable")
		base = fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s", user, host, port, dbname, ssl)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}
	// Connect to maintenance DB (postgres)
	u.Path = "/postgres"
	adminDSN := u.String()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Fatalf("close admin connection: %v", err)
		}
	})
	name := fmt.Sprintf("cg_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + pqQuoteIdent(name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	// Build DSN for the new database and wait until it is connectable
	u.Path = "/" + name
	dsn := u.String()
	// Retry ping a few times; some environments need a moment before new DB is visible
	for i := 0; i < 20; i++ {
		testConn, err := sql.Open("pgx", dsn)
		if err == nil {
			if err = testConn.Ping(); err == nil {
				if cerr := testConn.Close(); cerr != nil {
					t.Fatalf("close test connection: %v", cerr)
				}
				break
			}
			if cerr := testConn.Close(); cerr != nil {
				t.Fatalf("close test connection: %v", cerr)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Connect via app DB helper with explicit DSN (runs migrations)
	app, err := db.New(dsn)
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatalf("close app database: %v", err)
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pqQuoteIdent(name)); err != nil {
			t.Fatalf("drop database %s: %v", name, err)
		}
	})
	return app
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// simple identifier quoting
func pqQuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// split out for testability; replace with time.Now().UnixNano()
// no-op
