package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CLILoginState captures the persisted state for CLI OAuth flows.
type CLILoginState struct {
	State          string
	Code           sql.NullString
	CodeVerifier   sql.NullString
	LoginToken     sql.NullString
	LoginEmail     sql.NullString
	LoginName      sql.NullString
	LoginExpiresAt sql.NullTime
	MFAVerifiedAt  sql.NullTime
	MFAMethodID    sql.NullInt64
	LoginError     sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StoreCLILoginState upserts the state with the generated PKCE verifier.
func (d *Database) StoreCLILoginState(state, codeVerifier string) error {
	_, err := d.exec(`
        INSERT INTO cli_login_states (state, code_verifier, created_at, updated_at)
        VALUES (?, ?, NOW(), NOW())
        ON CONFLICT (state)
        DO UPDATE SET code_verifier = EXCLUDED.code_verifier,
                      code = NULL,
                      login_token = NULL,
                      login_email = NULL,
                      login_name = NULL,
                      login_expires_at = NULL,
                      mfa_verified_at = NULL,
                      mfa_method_id = NULL,
                      login_error = NULL,
                      updated_at = NOW()
    `, state, codeVerifier)
	if err != nil {
		return fmt.Errorf("failed to store CLI login state: %w", err)
	}
	return nil
}

// UpdateCLILoginCode records the authorization code returned by the IdP.
func (d *Database) UpdateCLILoginCode(state, code string) error {
	_, err := d.exec(`UPDATE cli_login_states SET code = ?, updated_at = NOW() WHERE state = ?`, code, state)
	if err != nil {
		return fmt.Errorf("failed to update CLI login code: %w", err)
	}
	return nil
}

// SaveCLILoginResult persists the successful login response after MFA verification.
func (d *Database) SaveCLILoginResult(state, email, name string, methodID *int64) error {
	if err := d.SetCLILoginProfile(state, email, name); err != nil {
		return err
	}
	return d.MarkCLILoginVerified(state, methodID)
}

// SetCLILoginProfile stores the OAuth exchange result so the CLI can poll for completion while
// additional MFA checks (or enrollment) are performed.
func (d *Database) SetCLILoginProfile(state, email, name string) error {
	_, err := d.exec(`
        UPDATE cli_login_states
        SET code = NULL,
            code_verifier = NULL,
            login_email = ?,
            login_name = ?,
            login_error = NULL,
            updated_at = NOW()
        WHERE state = ?
    `, email, name, state)
	if err != nil {
		return fmt.Errorf("failed to store CLI login profile: %w", err)
	}
	return nil
}

// MarkCLILoginVerified records that the CLI login has satisfied MFA requirements.
func (d *Database) MarkCLILoginVerified(state string, methodID *int64) error {
	_, err := d.exec(`
        UPDATE cli_login_states
        SET mfa_verified_at = NOW(),
            mfa_method_id = ?,
            login_error = NULL,
            updated_at = NOW()
        WHERE state = ?
    `, nullableInt64(methodID), state)
	if err != nil {
		return fmt.Errorf("failed to mark CLI login verified: %w", err)
	}
	return nil
}

// FailCLILoginState stores a terminal error for a CLI login attempt so polling clients can
// stop waiting and surface the failure message.
func (d *Database) FailCLILoginState(state, reason string) error {
	_, err := d.exec(`
        UPDATE cli_login_states
        SET login_error = ?,
            updated_at = NOW()
        WHERE state = ?
    `, nullableString(reason, 255), state)
	if err != nil {
		return fmt.Errorf("failed to mark CLI login failed: %w", err)
	}
	return nil
}

// GetCLILoginState retrieves the persisted CLI login state for the given key.
func (d *Database) GetCLILoginState(state string) (*CLILoginState, error) {
	query := `
        SELECT state, code, code_verifier, login_token, login_email, login_name,
               login_expires_at, mfa_verified_at, mfa_method_id, login_error,
               created_at, updated_at
        FROM cli_login_states WHERE state = ?
    `

	var record CLILoginState
	err := d.queryRow(query, state).Scan(
		&record.State,
		&record.Code,
		&record.CodeVerifier,
		&record.LoginToken,
		&record.LoginEmail,
		&record.LoginName,
		&record.LoginExpiresAt,
		&record.MFAVerifiedAt,
		&record.MFAMethodID,
		&record.LoginError,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get CLI login state: %w", err)
	}
	return &record, nil
}

// DeleteCLILoginState removes the stored CLI login state.
func (d *Database) DeleteCLILoginState(state string) error {
	_, err := d.exec(`DELETE FROM cli_login_states WHERE state = ?`, state)
	if err != nil {
		return fmt.Errorf("failed to delete CLI login state: %w", err)
	}
	return nil
}

func nullableInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
