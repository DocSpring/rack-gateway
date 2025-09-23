package db

import (
	"database/sql"
	"fmt"
)

// SaveCLILoginCode stores an authorization code for a given state
func (d *Database) SaveCLILoginCode(state, code string) error {
	// Upsert by state
	_, err := d.exec(`INSERT INTO cli_login_states (state, code) VALUES (?, ?) ON CONFLICT (state) DO UPDATE SET code = EXCLUDED.code`, state, code)
	if err != nil {
		return fmt.Errorf("failed to save CLI login code: %w", err)
	}
	return nil
}

// GetCLILoginCode retrieves and returns the code for a given state, and a boolean indicating existence
func (d *Database) GetCLILoginCode(state string) (string, bool, error) {
	var code string
	err := d.queryRow(`SELECT code FROM cli_login_states WHERE state = ?`, state).Scan(&code)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to get CLI login code: %w", err)
	}
	return code, true, nil
}

// DeleteCLILoginCode removes a stored code for a given state
func (d *Database) DeleteCLILoginCode(state string) error {
	_, err := d.exec(`DELETE FROM cli_login_states WHERE state = ?`, state)
	if err != nil {
		return fmt.Errorf("failed to delete CLI login code: %w", err)
	}
	return nil
}
