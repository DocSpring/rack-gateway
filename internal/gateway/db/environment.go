package db

import "database/sql"

// CurrentEnvironment returns the environment string stored in the metadata table, if present.
func (d *Database) CurrentEnvironment() (string, error) {
	row := d.queryRow(`SELECT environment FROM rgw_internal_metadata WHERE id = TRUE`)
	var env sql.NullString
	if err := row.Scan(&env); err != nil {
		if err == sql.ErrNoRows {
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
	env := "production"
	if isDev {
		env = "development"
	}
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
