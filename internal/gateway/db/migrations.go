package db

import (
	"context"
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
	if _, err := d.db.Exec(`SELECT pg_advisory_lock($1)`, AdvisoryLockMigration); err != nil {
		return err
	}
	defer func() {
		_, _ = d.db.Exec(`SELECT pg_advisory_unlock($1)`, AdvisoryLockMigration)
	}()

	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}
	// Base schema is defined in the timestamped init SQL; apply migrations in order.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	// read applied
	applied := map[string]bool{}
	rows, err := d.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			if err := rows.Close(); err != nil {
				return err
			}
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
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
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		// Extract just the timestamp prefix (before first underscore)
		if idx := strings.Index(version, "_"); idx > 0 {
			version = version[:idx]
		}
		if applied[version] {
			continue
		}
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
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	// Run River migrations after SQL migrations complete
	if err := d.migrateRiver(context.Background()); err != nil {
		return fmt.Errorf("failed to run River migrations: %w", err)
	}

	return nil
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
