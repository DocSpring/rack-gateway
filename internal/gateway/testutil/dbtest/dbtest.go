package dbtest

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// NewDatabase creates a unique temporary Postgres database for a test, copied from a fully migrated
// template (see template.go), and returns a connected *db.Database. The database is dropped on test cleanup.
// Sets TEST_DATABASE_URL env var to the created database DSN for subprocesses.
func NewDatabase(t *testing.T) *db.Database {
	t.Helper()
	ensureColumnEncryptionKey(t)
	baseDSN := getBaseDSN()
	admin, adminCleanup := setupAdminConnection(t, baseDSN)

	dbName := generateTestDBName()
	cloneTemplate(t, admin, dbName, ensureTemplate(t, admin, baseDSN))

	dsn := buildTestDSN(t, baseDSN, dbName)
	waitForDatabaseReady(t, dsn)

	app := connectAppDatabase(t, dsn)
	registerCleanup(t, app, admin, adminCleanup, dbName)

	return app
}

// ensureColumnEncryptionKey gives tests (and the gateways they start) the published development key unless
// the environment already sets one: every Database needs a key.
func ensureColumnEncryptionKey(t *testing.T) {
	t.Helper()
	if os.Getenv(colcrypt.EnvVar) != "" {
		return
	}
	if err := os.Setenv(colcrypt.EnvVar, colcrypt.DevKey); err != nil {
		t.Fatalf("set %s: %v", colcrypt.EnvVar, err)
	}
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
	// Limit admin connection pool to avoid exhaustion during parallel tests
	admin.SetMaxOpenConns(2)
	admin.SetMaxIdleConns(1)
	admin.SetConnMaxLifetime(5 * time.Minute)

	return admin, func() {
		if err := admin.Close(); err != nil {
			t.Fatalf("close admin connection: %v", err)
		}
	}
}

func generateTestDBName() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // never returns an error (crashes the program instead)
	// Mask to ensure we stay within int64 positive range (0 to MaxInt64)
	// G115: Masked to prevent overflow
	randomNum := int64(binary.LittleEndian.Uint64(b[:]) & 0x7FFFFFFFFFFFFFFF)
	return fmt.Sprintf("rgw_test_%d_%d", time.Now().UnixNano(), randomNum)
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
	err := retry(15*time.Second, 50*time.Millisecond, func() (bool, error) {
		return true, pingDatabase(dsn)
	})
	if err != nil {
		t.Fatalf("database did not become ready in time: %v", err)
	}
}

func pingDatabase(dsn string) error {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	return errors.Join(conn.Ping(), conn.Close())
}

// testPoolConfig is the connection pool of each test database.
var testPoolConfig = db.PoolConfig{
	MaxOpenConns:    4,
	MaxIdleConns:    1,
	ConnMaxLifetime: 5 * time.Minute,
	ConnMaxIdleTime: time.Minute,
}

func connectAppDatabase(t *testing.T, dsn string) *db.Database {
	t.Helper()

	// The database is a copy of the migrated template, so there's nothing to migrate. Tests get a small pool rather
	// than the production defaults: packages run in parallel, each with many databases on one shared Postgres, and
	// pgx opens its idle connections up front.
	app, err := db.NewWithPoolConfigAndMigration(dsn, &testPoolConfig, false)
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	// Set TEST_DATABASE_URL for subprocesses
	if err := os.Setenv("TEST_DATABASE_URL", dsn); err != nil {
		t.Fatalf("set TEST_DATABASE_URL: %v", err)
	}
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
		const terminateSQL = `
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE pg_stat_activity.datname = $1
			AND pid <> pg_backend_pid()`
		if _, err := admin.Exec(terminateSQL, dbName); err != nil {
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
