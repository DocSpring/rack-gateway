package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateAll applies embedded migrations in lexical order using a simple schema_migrations table.
func (d *Database) migrateAll() error {
	// Acquire a cluster-wide advisory lock to serialize migration runs.
	if err := d.acquireMigrationLock(); err != nil {
		return err
	}
	defer d.releaseMigrationLock()

	if err := d.createMigrationsTable(); err != nil {
		return err
	}

	applied, err := d.loadAppliedMigrations()
	if err != nil {
		return err
	}

	migrationFiles, err := d.loadMigrationFiles()
	if err != nil {
		return err
	}

	if err := d.applyPendingMigrations(migrationFiles, applied); err != nil {
		return err
	}

	// Run River migrations after SQL migrations complete
	if err := d.migrateRiver(context.Background()); err != nil {
		return fmt.Errorf("failed to run River migrations: %w", err)
	}

	return nil
}

func (d *Database) acquireMigrationLock() error {
	_, err := d.db.Exec(`SELECT pg_advisory_lock($1)`, AdvisoryLockMigration)
	return err
}

func (d *Database) releaseMigrationLock() {
	_, _ = d.db.Exec(`SELECT pg_advisory_unlock($1)`, AdvisoryLockMigration)
}

func (d *Database) createMigrationsTable() error {
	createSQL := `CREATE TABLE IF NOT EXISTS schema_migrations ` +
		`(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`
	_, err := d.db.Exec(createSQL)
	return err
}

func (d *Database) loadAppliedMigrations() (map[string]bool, error) {
	applied := map[string]bool{}
	rows, err := d.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}

	return applied, rows.Err()
}

func (d *Database) loadMigrationFiles() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".sql") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (d *Database) applyPendingMigrations(files []string, applied map[string]bool) error {
	for _, name := range files {
		version := extractVersionFromFilename(name)
		if applied[version] {
			continue
		}

		if err := d.applyMigration(name, version); err != nil {
			return err
		}
	}
	return nil
}

func extractVersionFromFilename(name string) string {
	version := strings.TrimSuffix(name, ".sql")
	// Extract just the timestamp prefix (before first underscore)
	if idx := strings.Index(version, "_"); idx > 0 {
		version = version[:idx]
	}
	return version
}

func (d *Database) applyMigration(name, version string) error {
	sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(string(sqlBytes)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s failed: %w", name, err)
	}

	if err := d.recordMigration(tx, version); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (d *Database) recordMigration(tx *sql.Tx, version string) error {
	_, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
	return err
}

// migrateRiver runs River's database migrations using the rivermigrate package.
// This creates the necessary tables (river_queue, river_job, etc.) for River job processing.
func (d *Database) migrateRiver(ctx context.Context) error {
	// Create River migrator using pgxpool connection
	migrator, err := rivermigrate.New(riverpgxv5.New(d.pool), nil)
	if err != nil {
		return fmt.Errorf("failed to create River migrator: %w", err)
	}

	// Migrate to latest version (no TargetVersion means migrate all the way up)
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{})
	if err != nil {
		return fmt.Errorf("failed to run River migrations: %w", err)
	}

	// Log migration results if any migrations were applied (only version numbers, not full SQL)
	if len(res.Versions) > 0 {
		versionNumbers := make([]int, 0, len(res.Versions))
		for _, v := range res.Versions {
			versionNumbers = append(versionNumbers, v.Version)
		}
		log.Printf("Applied %d River migration(s): %v", len(res.Versions), versionNumbers)
	}

	return nil
}
