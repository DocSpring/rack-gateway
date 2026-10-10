package dbtest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Running every migration (ours and River's) for each test was most of the Go test suite's time. Instead, one
// fully migrated template database is built per migrations fingerprint and each test gets a copy of it
// (CREATE DATABASE ... TEMPLATE), which takes milliseconds. Parallel test processes share the template: an
// advisory lock serializes building it, and it is only used once it's marked IS_TEMPLATE after migrating.

// templateName names the template for the current migrations and River version.
func templateName(t *testing.T) string {
	t.Helper()
	fingerprint, err := db.MigrationsFingerprint()
	if err != nil {
		t.Fatalf("fingerprint migrations: %v", err)
	}
	sum := sha256.Sum256([]byte(fingerprint + "|" + riverVersion()))
	return "rgw_template_" + hex.EncodeToString(sum[:])[:16]
}

// riverVersion is part of the fingerprint because the template also holds River's migrations.
func riverVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/riverqueue/river" {
			return dep.Version
		}
	}
	return "unknown"
}

// ensureTemplate returns the migrated template database, building it if it doesn't exist yet.
func ensureTemplate(t *testing.T, admin *sql.DB, baseDSN string) string {
	t.Helper()
	name := templateName(t)
	ctx := context.Background()

	conn, err := admin.Conn(ctx)
	if err != nil {
		t.Fatalf("template connection: %v", err)
	}
	// Advisory locks belong to the session, so lock and unlock on this connection. Closing it also releases
	// the lock if the test fails part way.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext($1))", name); err != nil {
		t.Fatalf("lock template %s: %v", name, err)
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtext($1))", name); err != nil {
			t.Logf("unlock template %s: %v", name, err)
		}
		if err := conn.Close(); err != nil {
			t.Logf("close template connection: %v", err)
		}
	}()

	var isTemplate bool
	err = conn.QueryRowContext(ctx, "SELECT datistemplate FROM pg_database WHERE datname = $1", name).Scan(&isTemplate)
	switch {
	case err == nil && isTemplate:
		return name
	case err == nil:
		// A build that never finished (e.g. an interrupted test run): start again.
		if _, err := conn.ExecContext(ctx, "DROP DATABASE "+pqQuoteIdent(name)); err != nil {
			t.Fatalf("drop unfinished template %s: %v", name, err)
		}
	case !errors.Is(err, sql.ErrNoRows):
		t.Fatalf("look up template %s: %v", name, err)
	}

	if _, err := conn.ExecContext(ctx, "CREATE DATABASE "+pqQuoteIdent(name)); err != nil {
		t.Fatalf("create template %s: %v", name, err)
	}
	migrateTemplate(t, buildTestDSN(t, baseDSN, name))
	if _, err := conn.ExecContext(ctx, "ALTER DATABASE "+pqQuoteIdent(name)+" IS_TEMPLATE true"); err != nil {
		t.Fatalf("mark template %s: %v", name, err)
	}
	return name
}

// migrateTemplate runs every migration in the template and closes all its connections, so it can be copied.
func migrateTemplate(t *testing.T, dsn string) {
	t.Helper()
	waitForDatabaseReady(t, dsn)

	rolesDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open template for audit roles: %v", err)
	}
	createAuditRoles(t, rolesDB)
	if err := rolesDB.Close(); err != nil {
		t.Fatalf("close template roles connection: %v", err)
	}

	migrated, err := db.NewWithPoolConfigAndMigration(dsn, nil, true)
	if err != nil {
		t.Fatalf("migrate template: %v", err)
	}
	if err := migrated.Close(); err != nil {
		t.Fatalf("close template: %v", err)
	}
}

// cloneTemplate creates name as a copy of the template. Copying fails while another session is connected to
// the template (e.g. autovacuum), so it retries briefly.
func cloneTemplate(t *testing.T, admin *sql.DB, name, template string) {
	t.Helper()
	query := strings.Join([]string{"CREATE DATABASE", pqQuoteIdent(name), "TEMPLATE", pqQuoteIdent(template)}, " ")
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		if _, err = admin.Exec(query); err == nil {
			return
		}
		if !strings.Contains(err.Error(), "is being accessed by other users") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("create database %s from template %s: %v", name, template, err)
}
