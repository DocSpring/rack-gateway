package db

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// EnvironmentName is the environment marker for a development (isDev) or production database.
func EnvironmentName(isDev bool) string {
	if isDev {
		return "development"
	}
	return "production"
}

// CurrentEnvironment returns the environment string stored in the metadata table, or "" when there is none (also
// in a database that hasn't been migrated yet).
func (d *Database) CurrentEnvironment() (string, error) {
	row := d.queryRow(`SELECT environment FROM rgw_internal_metadata WHERE id = TRUE`)
	var env sql.NullString
	if err := row.Scan(&env); err != nil {
		var pgErr *pgconn.PgError
		if errors.Is(err, sql.ErrNoRows) || (errors.As(err, &pgErr) && pgErr.Code == "42P01") {
			return "", nil
		}
		return "", err
	}
	if !env.Valid {
		return "", nil
	}
	return env.String, nil
}

// SetEnvironment upserts the current environment marker (development/production).
func (d *Database) SetEnvironment(isDev bool) error {
	env := EnvironmentName(isDev)
	_, err := d.exec(`
		INSERT INTO rgw_internal_metadata (id, environment, updated_at)
		VALUES (TRUE, ?, NOW())
		ON CONFLICT (id)
		DO UPDATE SET environment = EXCLUDED.environment, updated_at = NOW()
	`, env)
	return err
}

// EnsureEnvironment sets the environment marker only if it has not been initialized yet.
func (d *Database) EnsureEnvironment(isDev bool) error {
	current, err := d.CurrentEnvironment()
	if err != nil {
		return err
	}
	if current == "" {
		return d.SetEnvironment(isDev)
	}
	return nil
}
