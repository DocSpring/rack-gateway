package db

import (
	"database/sql"
	"fmt"
	"time"
)

// cliLoginLive restricts queries to CLI logins started within the last 10 minutes.
const cliLoginLive = "created_at > NOW() - INTERVAL '10 minutes'"

// CLILoginState is a CLI login in progress. The state column is the gateway's own OAuth
// state for the Google leg; the CLI never needs it. The CLI is identified by its PKCE code
// challenge, its loopback redirect URI and its own state value.
type CLILoginState struct {
	State              string
	OAuthCode          sql.NullString
	OAuthCodeVerifier  sql.NullString
	CLICodeChallenge   string
	CLIRedirectURI     string
	CLIState           string
	InitiatorIP        sql.NullString
	InitiatorDevice    sql.NullString
	BrowserBindingHash sql.NullString
	EnrollmentRequired bool
	LoginEmail         sql.NullString
	LoginName          sql.NullString
	MFAVerifiedAt      sql.NullTime
	MFAMethodID        sql.NullInt64
	LoginError         sql.NullString
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NewCLILogin holds the values recorded when a CLI starts a login.
type NewCLILogin struct {
	State             string
	OAuthCodeVerifier string
	CLICodeChallenge  string
	CLIRedirectURI    string
	CLIState          string
	InitiatorIP       string
	InitiatorDevice   string
}

const cliLoginColumns = `state, oauth_code, oauth_code_verifier, cli_code_challenge, cli_redirect_uri, cli_state,
               initiator_ip, initiator_device, browser_binding_hash, enrollment_required, login_email, login_name,
               mfa_verified_at, mfa_method_id, login_error, created_at, updated_at`

// CreateCLILoginState records a new CLI login and removes expired ones.
func (d *Database) CreateCLILoginState(login NewCLILogin) error {
	if err := d.DeleteExpiredCLILoginStates(); err != nil {
		return err
	}
	_, err := d.exec(`
        INSERT INTO cli_login_states (
            state, oauth_code_verifier, cli_code_challenge, cli_redirect_uri, cli_state,
            initiator_ip, initiator_device, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
    `,
		login.State,
		login.OAuthCodeVerifier,
		login.CLICodeChallenge,
		login.CLIRedirectURI,
		login.CLIState,
		nullableString(login.InitiatorIP, 64),
		nullableString(login.InitiatorDevice, 64),
	)
	if err != nil {
		return fmt.Errorf("failed to store CLI login state: %w", err)
	}
	return nil
}

// BindCLILoginBrowser stores the identity provider's authorization code and binds the login to
// the browser that delivered it. It only succeeds once per login, so a replayed callback URL
// cannot bind a second browser. Returns false when the login is unknown, expired or already bound.
func (d *Database) BindCLILoginBrowser(state, oauthCode, bindingHash string) (bool, error) {
	res, err := d.exec(`
        UPDATE cli_login_states
        SET oauth_code = ?, browser_binding_hash = ?, updated_at = NOW()
        WHERE state = ? AND browser_binding_hash IS NULL AND `+cliLoginLive,
		oauthCode, bindingHash, state)
	if err != nil {
		return false, fmt.Errorf("failed to bind CLI login browser: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to bind CLI login browser: %w", err)
	}
	return rows == 1, nil
}

// SetCLILoginProfile stores the identity provider result and discards the authorization code.
func (d *Database) SetCLILoginProfile(state, email, name string) error {
	_, err := d.exec(`
        UPDATE cli_login_states
        SET oauth_code = NULL,
            oauth_code_verifier = NULL,
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

// MarkCLILoginEnrollmentRequired records that the user had no MFA factor when the login reached MFA,
// so a factor enrolled in the bound browser during this login may satisfy the login's MFA requirement.
func (d *Database) MarkCLILoginEnrollmentRequired(state string) error {
	_, err := d.exec(
		`UPDATE cli_login_states SET enrollment_required = TRUE, updated_at = NOW() WHERE state = ?`,
		state,
	)
	if err != nil {
		return fmt.Errorf("failed to mark CLI login enrollment required: %w", err)
	}
	return nil
}

// FailCLILoginState stores a terminal error for a CLI login attempt.
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

// GetCLILoginState retrieves a live (unexpired) CLI login.
func (d *Database) GetCLILoginState(state string) (*CLILoginState, error) {
	row := d.queryRow(`SELECT `+cliLoginColumns+` FROM cli_login_states WHERE state = ? AND `+cliLoginLive, state)
	record, err := scanCLILoginState(row)
	if err != nil {
		return nil, fmt.Errorf("failed to get CLI login state: %w", err)
	}
	return record, nil
}

// SetCLILoginCode stores the hash of a single-use login code for a verified CLI login.
// The code is valid for two minutes. Returns false when the login is not live or not verified.
func (d *Database) SetCLILoginCode(state, codeHash string) (bool, error) {
	res, err := d.exec(`
        UPDATE cli_login_states
        SET login_code_hash = ?,
            login_code_expires_at = NOW() + INTERVAL '2 minutes',
            updated_at = NOW()
        WHERE state = ? AND mfa_verified_at IS NOT NULL AND login_error IS NULL AND `+cliLoginLive,
		codeHash, state)
	if err != nil {
		return false, fmt.Errorf("failed to store CLI login code: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to store CLI login code: %w", err)
	}
	return rows == 1, nil
}

// ConsumeCLILoginCode atomically deletes and returns the live CLI login holding the login code.
// Each code can be redeemed at most once. Returns nil when no live login holds the code.
func (d *Database) ConsumeCLILoginCode(codeHash string) (*CLILoginState, error) {
	row := d.queryRow(`
        DELETE FROM cli_login_states
        WHERE login_code_hash = ? AND login_code_expires_at > NOW() AND `+cliLoginLive+`
        RETURNING `+cliLoginColumns, codeHash)
	record, err := scanCLILoginState(row)
	if err != nil {
		return nil, fmt.Errorf("failed to consume CLI login code: %w", err)
	}
	return record, nil
}

// DeleteExpiredCLILoginStates removes CLI logins older than the login time limit.
func (d *Database) DeleteExpiredCLILoginStates() error {
	if _, err := d.exec(`DELETE FROM cli_login_states WHERE NOT (` + cliLoginLive + `)`); err != nil {
		return fmt.Errorf("failed to delete expired CLI login states: %w", err)
	}
	return nil
}

func scanCLILoginState(row *sql.Row) (*CLILoginState, error) {
	var record CLILoginState
	err := row.Scan(
		&record.State,
		&record.OAuthCode,
		&record.OAuthCodeVerifier,
		&record.CLICodeChallenge,
		&record.CLIRedirectURI,
		&record.CLIState,
		&record.InitiatorIP,
		&record.InitiatorDevice,
		&record.BrowserBindingHash,
		&record.EnrollmentRequired,
		&record.LoginEmail,
		&record.LoginName,
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
		return nil, err
	}
	return &record, nil
}

func nullableInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
