package dbtest

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Reset truncates all application tables to provide a clean slate for tests.
// Uses TRUNCATE ... CASCADE so FK references are handled.
// Silently ignores tables that don't exist (for tests that run before migrations).
func Reset(t *testing.T, database *db.Database) {
	t.Helper()

	// Truncate each table individually, ignoring "does not exist" errors
	tables := []string{
		"api_tokens",
		"audit.audit_event",
		"audit.audit_event_aggregated",
		"cli_login_states",
		"mfa_attempts",
		"users",
	}

	for _, table := range tables {
		_, err := database.DB().Exec(fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", table))
		if err != nil && !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("failed to truncate %s: %v", table, err)
		}
	}

	// Reset audit sequence (ignore if doesn't exist)
	_, err := database.DB().Exec("ALTER SEQUENCE audit.audit_event_chain_index_seq RESTART WITH 0")
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("failed to reset audit sequence: %v", err)
	}
}

// NewDatabase creates a unique temporary Postgres database for a test, runs migrations,
// and returns a connected *db.Database. The database is dropped on test cleanup.
// Sets TEST_DATABASE_URL env var to the created database DSN for subprocesses.
func NewDatabase(t *testing.T) *db.Database {
	t.Helper()
	baseDSN := getBaseDSN()
	admin, adminCleanup := setupAdminConnection(t, baseDSN)

	dbName := generateTestDBName()
	createDatabase(t, admin, dbName)

	dsn := buildTestDSN(t, baseDSN, dbName)
	waitForDatabaseReady(t, dsn)

	app := connectAppDatabase(t, dsn)
	registerCleanup(t, app, admin, adminCleanup, dbName)

	return app
}

func getBaseDSN() string {
	base := os.Getenv("TEST_DATABASE_URL")
	if base != "" {
		return base
	}

	// Build from PG* env
	host := getenv("PGHOST", "localhost")
	port := getenv("PGPORT", "5432")
	user := getenv("PGUSER", getenv("USER", "postgres"))
	dbname := getenv("PGDATABASE", user)
	ssl := getenv("PGSSLMODE", "disable")
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s", user, host, port, dbname, ssl)
}

func setupAdminConnection(t *testing.T, baseDSN string) (*sql.DB, func()) {
	t.Helper()
	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/postgres"
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	return admin, func() {
		if err := admin.Close(); err != nil {
			t.Fatalf("close admin connection: %v", err)
		}
	}
}

func generateTestDBName() string {
	return fmt.Sprintf("rgw_test_%d_%d", time.Now().UnixNano(), rand.Int63())
}

func createDatabase(t *testing.T, admin *sql.DB, name string) {
	t.Helper()
	if _, err := admin.Exec("CREATE DATABASE " + pqQuoteIdent(name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
}

func buildTestDSN(t *testing.T, baseDSN, dbName string) string {
	t.Helper()
	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("invalid base DSN: %v", err)
	}
	u.Path = "/" + dbName
	return u.String()
}

func waitForDatabaseReady(t *testing.T, dsn string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		testConn, err := sql.Open("pgx", dsn)
		if err == nil {
			if err = testConn.Ping(); err == nil {
				if cerr := testConn.Close(); cerr != nil {
					t.Fatalf("close test connection: %v", cerr)
				}
				return
			}
			if cerr := testConn.Close(); cerr != nil {
				t.Fatalf("close test connection: %v", cerr)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("database did not become ready in time")
}

func connectAppDatabase(t *testing.T, dsn string) *db.Database {
	t.Helper()

	// Create audit roles before running migrations
	// These roles are required by migration 20251008173116_audit_logs.sql
	testDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db for audit roles: %v", err)
	}
	defer testDB.Close() //nolint:errcheck // test cleanup

	createAuditRoles(t, testDB)

	// Always run migrations for test databases
	app, err := db.NewWithPoolConfigAndMigration(dsn, nil, true)
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	// Set TEST_DATABASE_URL for subprocesses
	os.Setenv("TEST_DATABASE_URL", dsn) //nolint:errcheck // ignore error
	return app
}

func createAuditRoles(t *testing.T, sqlDB *sql.DB) {
	t.Helper()

	// Create the three audit roles (IF NOT EXISTS for idempotency)
	// These match the roles created by setup-audit-roles.sh
	_, err := sqlDB.Exec(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_owner') THEN
				CREATE ROLE audit_owner NOLOGIN;
			END IF;
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_writer') THEN
				CREATE ROLE audit_writer NOLOGIN;
			END IF;
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'audit_reader') THEN
				CREATE ROLE audit_reader NOLOGIN;
			END IF;
		END
		$$;

		-- Grant audit_owner to postgres user (equivalent to rack_gateway_admin in production)
		GRANT audit_owner TO postgres;
	`)
	if err != nil {
		t.Fatalf("create audit roles: %v", err)
	}
}

func registerCleanup(t *testing.T, app *db.Database, admin *sql.DB, adminCleanup func(), dbName string) {
	t.Helper()
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatalf("close app database: %v", err)
		}

		// Terminate all connections to the database before dropping
		// This prevents DROP DATABASE from hanging on active connections
		terminateSQL := fmt.Sprintf(`
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE pg_stat_activity.datname = %s
			AND pid <> pg_backend_pid()`,
			pqQuoteLiteral(dbName))
		if _, err := admin.Exec(terminateSQL); err != nil {
			// Don't fail on this - database might not exist or have no connections
			t.Logf("Warning: failed to terminate connections: %v", err)
		}

		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pqQuoteIdent(dbName)); err != nil {
			t.Fatalf("drop database %s: %v", dbName, err)
		}
		// Close admin connection after dropping the test database
		adminCleanup()
	})
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// simple identifier quoting
func pqQuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// simple string literal quoting
func pqQuoteLiteral(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

// split out for testability; replace with time.Now().UnixNano()
// no-op
