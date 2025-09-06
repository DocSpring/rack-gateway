package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Database wraps the SQL database connection
type Database struct {
	db *sql.DB
}

// User represents a user in the system
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Suspended bool      `json:"suspended"`
}

// APIToken represents an API token for CI/CD
type APIToken struct {
	ID          int64      `json:"id"`
	TokenHash   string     `json:"-"` // Never expose the actual token
	Name        string     `json:"name"`
	UserID      int64      `json:"user_id"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	UserEmail      string    `json:"user_email"`
	UserName       string    `json:"user_name,omitempty"`
	ActionType     string    `json:"action_type"` // "convox_api", "user_management", "auth"
	Action         string    `json:"action"`      // e.g., "env.get", "user.create", "auth.failed"
	Command        string    `json:"command,omitempty"`
	Resource       string    `json:"resource,omitempty"`
	Details        string    `json:"details,omitempty"` // JSON string
	IPAddress      string    `json:"ip_address,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	Status         string    `json:"status"` // "success", "denied", "error", "blocked"
	ResponseTimeMs int       `json:"response_time_ms"`
}

// New creates a new database connection
func New(dbPath string) (*Database, error) {
	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &Database{db: db}

	// Initialize schema
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return d, nil
}

// initSchema creates the database tables if they don't exist
func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		roles TEXT NOT NULL, -- JSON array of roles
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		suspended BOOLEAN DEFAULT FALSE
	);

    CREATE TABLE IF NOT EXISTS api_tokens (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        token_hash TEXT NOT NULL UNIQUE,
        name TEXT NOT NULL,
        user_id INTEGER NOT NULL,
        permissions TEXT NOT NULL, -- JSON array of permissions
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        expires_at DATETIME,
        last_used_at DATETIME,
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS audit_logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        user_email TEXT NOT NULL,
        user_name TEXT,
        action_type TEXT NOT NULL,
        action TEXT NOT NULL,
        command TEXT,
        resource TEXT,
        details TEXT, -- JSON with command details
        ip_address TEXT,
        user_agent TEXT,
        status TEXT NOT NULL,
        response_time_ms INTEGER
    );

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_user_email ON audit_logs(user_email);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_action_type ON audit_logs(action_type);

    -- CLI login interim codes (state -> code)
    CREATE TABLE IF NOT EXISTS cli_login_states (
        state TEXT PRIMARY KEY,
        code TEXT NOT NULL,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );

    `

	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	// No runtime migrations; schema is defined above for greenfield usage
	return nil
}

// SaveCLILoginCode stores an authorization code for a given state
func (d *Database) SaveCLILoginCode(state, code string) error {
	_, err := d.db.Exec(`INSERT OR REPLACE INTO cli_login_states (state, code) VALUES (?, ?)`, state, code)
	if err != nil {
		return fmt.Errorf("failed to save CLI login code: %w", err)
	}
	return nil
}

// GetCLILoginCode retrieves and returns the code for a given state, and a boolean indicating existence
func (d *Database) GetCLILoginCode(state string) (string, bool, error) {
	var code string
	err := d.db.QueryRow(`SELECT code FROM cli_login_states WHERE state = ?`, state).Scan(&code)
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
	_, err := d.db.Exec(`DELETE FROM cli_login_states WHERE state = ?`, state)
	if err != nil {
		return fmt.Errorf("failed to delete CLI login code: %w", err)
	}
	return nil
}

// InitializeAdmin creates the initial admin user if no users exist
func (d *Database) InitializeAdmin(email, name string) error {
	// Check if any users exist
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}

	// If users exist, don't create admin
	if count > 0 {
		return nil
	}

	// Create admin user
	roles := []string{"admin"}
	rolesJSON, _ := json.Marshal(roles)

	_, err = d.db.Exec(
		"INSERT INTO users (email, name, roles) VALUES (?, ?, ?)",
		email, name, string(rolesJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	return nil
}

// GetUserByID retrieves a user by ID
func (d *Database) GetUserByID(id int64) (*User, error) {
	var user User
	var rolesJSON string

	err := d.db.QueryRow(
		"SELECT id, email, name, roles, created_at, updated_at, suspended FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	return &user, nil
}

// GetUser retrieves a user by email
func (d *Database) GetUser(email string) (*User, error) {
	var user User
	var rolesJSON string

	err := d.db.QueryRow(
		"SELECT id, email, name, roles, created_at, updated_at, suspended FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Email, &user.Name, &rolesJSON, &user.CreatedAt, &user.UpdatedAt, &user.Suspended)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
	}

	return &user, nil
}

// CreateUser creates a new user
func (d *Database) CreateUser(email, name string, roles []string) (*User, error) {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal roles: %w", err)
	}

	result, err := d.db.Exec(
		"INSERT INTO users (email, name, roles) VALUES (?, ?, ?)",
		email, name, string(rolesJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:        id,
		Email:     email,
		Name:      name,
		Roles:     roles,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// UpdateUserRoles updates a user's roles
func (d *Database) UpdateUserRoles(email string, roles []string) error {
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("failed to marshal roles: %w", err)
	}

	_, err = d.db.Exec(
		"UPDATE users SET roles = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?",
		string(rolesJSON), email,
	)
	if err != nil {
		return fmt.Errorf("failed to update user roles: %w", err)
	}

	return nil
}

// DeleteUser removes a user from the database
func (d *Database) DeleteUser(email string) error {
	_, err := d.db.Exec("DELETE FROM users WHERE email = ?", email)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// ListUsers returns all users
func (d *Database) ListUsers() ([]*User, error) {
	rows, err := d.db.Query(
		"SELECT id, email, name, roles, created_at, updated_at, suspended FROM users ORDER BY email",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON string

		err := rows.Scan(&user.ID, &user.Email, &user.Name, &rolesJSON,
			&user.CreatedAt, &user.UpdatedAt, &user.Suspended)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}

		if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
			return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
		}

		users = append(users, &user)
	}

	return users, nil
}

// CreateAuditLog creates a new audit log entry
func (d *Database) CreateAuditLog(log *AuditLog) error {
	_, err := d.db.Exec(
		`INSERT INTO audit_logs (
            user_email, user_name, action_type, action, command, resource, 
            details, ip_address, user_agent, status, response_time_ms
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.UserEmail, log.UserName, log.ActionType, log.Action, log.Command, log.Resource,
		log.Details, log.IPAddress, log.UserAgent, log.Status, log.ResponseTimeMs,
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}
	return nil
}

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(userEmail string, since time.Time, limit int) ([]*AuditLog, error) {
	query := `
        SELECT id, timestamp, user_email, COALESCE(user_name, ''), action_type, action, 
               COALESCE(command, ''), COALESCE(resource, ''), COALESCE(details, ''), COALESCE(ip_address, ''), 
               COALESCE(user_agent, ''), status, response_time_ms
        FROM audit_logs
        WHERE 1=1
    `
	args := []interface{}{}

	if userEmail != "" {
		query += " AND user_email = ?"
		args = append(args, userEmail)
	}

	if !since.IsZero() {
		// Normalize to UTC to match SQLite CURRENT_TIMESTAMP storage
		query += " AND timestamp >= ?"
		args = append(args, since.UTC())
	}

	query += " ORDER BY timestamp DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName,
			&log.ActionType, &log.Action, &log.Command, &log.Resource, &log.Details,
			&log.IPAddress, &log.UserAgent, &log.Status, &log.ResponseTimeMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// CreateAPIToken creates a new API token
func (d *Database) CreateAPIToken(tokenHash, name string, userID int64, permissions []string, expiresAt *time.Time) (*APIToken, error) {
	permissionsJSON, err := json.Marshal(permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal permissions: %w", err)
	}

	var expVal interface{}
	if expiresAt != nil {
		expVal = *expiresAt
	} else {
		expVal = nil
	}

	result, err := d.db.Exec(
		"INSERT INTO api_tokens (token_hash, name, user_id, permissions, expires_at) VALUES (?, ?, ?, ?, ?)",
		tokenHash, name, userID, string(permissionsJSON), expVal,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}

	id, _ := result.LastInsertId()
	return &APIToken{
		ID:          id,
		TokenHash:   tokenHash,
		Name:        name,
		UserID:      userID,
		Permissions: permissions,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
	}, nil
}

// GetAPITokenByHash retrieves an API token by its hash
func (d *Database) GetAPITokenByHash(tokenHash string) (*APIToken, error) {
	var token APIToken
	var permissionsJSON string
	var expiresAtNull sql.NullTime
	var lastUsedAtNull sql.NullTime

	err := d.db.QueryRow(
		"SELECT id, token_hash, name, user_id, permissions, created_at, expires_at, last_used_at FROM api_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID, &permissionsJSON,
		&token.CreatedAt, &expiresAtNull, &lastUsedAtNull)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API token: %w", err)
	}

	if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
	}

	if expiresAtNull.Valid {
		t := expiresAtNull.Time
		token.ExpiresAt = &t
	}
	if lastUsedAtNull.Valid {
		token.LastUsedAt = &lastUsedAtNull.Time
	}

	return &token, nil
}

// ListAPITokensByUser returns all API tokens for a user
func (d *Database) ListAPITokensByUser(userID int64) ([]*APIToken, error) {
	rows, err := d.db.Query(
		"SELECT id, token_hash, name, user_id, permissions, created_at, expires_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list API tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*APIToken
	for rows.Next() {
		var token APIToken
		var permissionsJSON string
		var expiresAtNull sql.NullTime
		var lastUsedAtNull sql.NullTime

		err := rows.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID,
			&permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API token: %w", err)
		}

		if err := json.Unmarshal([]byte(permissionsJSON), &token.Permissions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal permissions: %w", err)
		}

		if expiresAtNull.Valid {
			t := expiresAtNull.Time
			token.ExpiresAt = &t
		}
		if lastUsedAtNull.Valid {
			token.LastUsedAt = &lastUsedAtNull.Time
		}

		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last used timestamp
func (d *Database) UpdateAPITokenLastUsed(tokenHash string) error {
	_, err := d.db.Exec(
		"UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE token_hash = ?",
		tokenHash,
	)
	if err != nil {
		return fmt.Errorf("failed to update API token last used: %w", err)
	}
	return nil
}

// DeleteAPIToken removes an API token
func (d *Database) DeleteAPIToken(id int64) error {
	_, err := d.db.Exec("DELETE FROM api_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API token: %w", err)
	}
	return nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// DB exposes the underlying *sql.DB for tests and advanced usage.
// Avoid using this in application code where higher-level helpers exist.
func (d *Database) DB() *sql.DB {
	return d.db
}
