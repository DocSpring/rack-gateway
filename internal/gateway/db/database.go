package db

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Database wraps the SQL database connection
type Database struct {
	db     *sql.DB
	driver string // always "pgx"
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
	ID              int64      `json:"id"`
	TokenHash       string     `json:"-"` // Never expose the actual token
	Name            string     `json:"name"`
	UserID          int64      `json:"user_id"`
	CreatedByUserID *int64     `json:"created_by_user_id,omitempty"`
	CreatedByEmail  string     `json:"created_by_email,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
	Permissions     []string   `json:"permissions"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       *time.Time `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	UserEmail      string    `json:"user_email"`
	UserName       string    `json:"user_name,omitempty"`
	ActionType     string    `json:"action_type"` // "convox", "users", "auth"
	Action         string    `json:"action"`      // e.g., "env.get", "user.create", "auth.failed"
	Command        string    `json:"command,omitempty"`
	Resource       string    `json:"resource,omitempty"`
	ResourceType   string    `json:"resource_type,omitempty"`
	Details        string    `json:"details,omitempty"` // JSON string
	IPAddress      string    `json:"ip_address,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	Status         string    `json:"status"`                  // "success", "denied", "error", "blocked"
	RBACDecision   string    `json:"rbac_decision,omitempty"` // "allow" or "deny"
	HTTPStatus     int       `json:"http_status,omitempty"`
	ResponseTimeMs int       `json:"response_time_ms"`
}

// New creates a new database connection
func New(dsn string) (*Database, error) {
	// Use provided DSN if it looks like Postgres, else use env var
	source := strings.TrimSpace(dsn)
	if source == "" || !(strings.HasPrefix(strings.ToLower(source), "postgres://") || strings.HasPrefix(strings.ToLower(source), "postgresql://")) {
		source = os.Getenv("DATABASE_URL")
	}
	if source == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	// Ensure appropriate sslmode: require in non-dev unless explicitly set
	source = ensureSSLMode(source)
	db, err := sql.Open("pgx", source)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}
	d := &Database{db: db, driver: "pgx"}
	if err := d.migrateAll(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return d, nil
}

// NewFromEnv builds a Postgres DSN from env if DATABASE_URL is unset.
func NewFromEnv() (*Database, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return New(dsn)
	}
	// Build from libpq-like env if present
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "postgres"
	}
	dbname := os.Getenv("PGDATABASE")
	if dbname == "" {
		dbname = user
	}
	// Respect PGSSLMODE if present; otherwise omit sslmode from DSN
	if ssl := strings.TrimSpace(os.Getenv("PGSSLMODE")); ssl != "" {
		dsn := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s", user, host, port, dbname, ssl)
		return New(dsn)
	}
	dsn := fmt.Sprintf("postgres://%s@%s:%s/%s", user, host, port, dbname)
	return New(dsn)
}

// ensureSSLMode appends an sslmode if missing. In non-dev (DEV_MODE != true) default to require.
func ensureSSLMode(dsn string) string {
	s := strings.TrimSpace(dsn)
	if s == "" {
		return dsn
	}
	// Only mutate sslmode when PGSSLMODE is explicitly set
	mode := strings.TrimSpace(os.Getenv("PGSSLMODE"))
	if mode == "" {
		return dsn
	}

	lower := strings.ToLower(s)

	// URL DSN: postgres:// or postgresql://
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(s)
		if err != nil {
			return dsn
		}
		q := u.Query()
		q.Set("sslmode", mode) // override or set
		u.RawQuery = q.Encode()
		return u.String()
	}

	// Keyword/libpq DSN: replace or append sslmode
	parts := strings.Fields(s)
	found := false
	for i, p := range parts {
		if strings.HasPrefix(strings.ToLower(p), "sslmode=") {
			parts[i] = "sslmode=" + mode
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, "sslmode="+mode)
	}
	return strings.Join(parts, " ")
}

// Embedded migrations for future use
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateAll applies embedded migrations in lexical order using a simple schema_migrations table.
func (d *Database) migrateAll() error {
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}
	// Base schema is defined in 0001_init.sql; apply migrations in order.
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
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
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
	return nil
}

// rebind converts ? placeholders to $1, $2 for Postgres driver
func (d *Database) rebind(q string) string {
	if d.driver != "pgx" {
		return q
	}
	var b strings.Builder
	n := 1
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			b.WriteString(fmt.Sprintf("$%d", n))
			n++
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func (d *Database) exec(q string, args ...interface{}) (sql.Result, error) {
	return d.db.Exec(d.rebind(q), args...)
}

func (d *Database) query(q string, args ...interface{}) (*sql.Rows, error) {
	return d.db.Query(d.rebind(q), args...)
}

func (d *Database) queryRow(q string, args ...interface{}) *sql.Row {
	return d.db.QueryRow(d.rebind(q), args...)
}

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

	var id int64
	if err := d.queryRow("INSERT INTO users (email, name, roles) VALUES (?, ?, ?) RETURNING id", email, name, string(rolesJSON)).Scan(&id); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}
	return nil
}

// GetUserByID retrieves a user by ID
func (d *Database) GetUserByID(id int64) (*User, error) {
	var user User
	var rolesJSON string

	err := d.queryRow(
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

	err := d.queryRow(
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

	var id int64
	if err := d.queryRow("INSERT INTO users (email, name, roles) VALUES (?, ?, ?) RETURNING id", email, name, string(rolesJSON)).Scan(&id); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
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

	_, err = d.exec(
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
	_, err := d.exec("DELETE FROM users WHERE email = ?", email)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// ListUsers returns all users
func (d *Database) ListUsers() ([]*User, error) {
	rows, err := d.query(
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
	_, err := d.exec(
		`INSERT INTO audit_logs (
            user_email, user_name, action_type, action, command, resource, resource_type,
            details, ip_address, user_agent, status, rbac_decision, http_status, response_time_ms
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.UserEmail, log.UserName, log.ActionType, log.Action, log.Command, log.Resource, log.ResourceType,
		log.Details, log.IPAddress, log.UserAgent, log.Status, log.RBACDecision, log.HTTPStatus, log.ResponseTimeMs,
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
               COALESCE(command, ''), COALESCE(resource, ''), COALESCE(resource_type, ''), COALESCE(details, ''),
               COALESCE(ip_address, ''), COALESCE(user_agent, ''), status, COALESCE(rbac_decision, ''), COALESCE(http_status, 0), response_time_ms
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

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName,
			&log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details,
			&log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetAuditLogsPaged retrieves audit logs with limit and offset for pagination
func (d *Database) GetAuditLogsPaged(userEmail string, since time.Time, limit, offset int) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	query := `
        SELECT id, timestamp, user_email, COALESCE(user_name, ''), action_type, action,
               COALESCE(command, ''), COALESCE(resource, ''), COALESCE(resource_type, ''),
               COALESCE(details, ''), COALESCE(ip_address, ''), COALESCE(user_agent, ''), status, COALESCE(rbac_decision, ''), COALESCE(http_status, 0), response_time_ms
        FROM audit_logs
        WHERE 1=1
    `
	args := []interface{}{}
	if userEmail != "" {
		query += " AND user_email = ?"
		args = append(args, userEmail)
	}
	if !since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, since.UTC())
	}
	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details, &log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs); err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, &log)
	}
	return logs, nil
}

// CreateAPIToken creates a new API token
func (d *Database) CreateAPIToken(tokenHash, name string, userID int64, permissions []string, expiresAt *time.Time, createdByUserID *int64) (*APIToken, error) {
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

	var id int64
	if err := d.queryRow("INSERT INTO api_tokens (token_hash, name, user_id, permissions, expires_at, created_by_user_id) VALUES (?, ?, ?, ?, ?, ?) RETURNING id", tokenHash, name, userID, string(permissionsJSON), expVal, createdByUserID).Scan(&id); err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}
	return &APIToken{
		ID:              id,
		TokenHash:       tokenHash,
		Name:            name,
		UserID:          userID,
		CreatedByUserID: createdByUserID,
		Permissions:     permissions,
		CreatedAt:       time.Now(),
		ExpiresAt:       expiresAt,
	}, nil
}

// GetAPITokenByHash retrieves an API token by its hash
func (d *Database) GetAPITokenByHash(tokenHash string) (*APIToken, error) {
	var token APIToken
	var permissionsJSON string
	var expiresAtNull sql.NullTime
	var lastUsedAtNull sql.NullTime

	var createdByNull sql.NullInt64
	err := d.queryRow(
		"SELECT id, token_hash, name, user_id, permissions, created_at, expires_at, last_used_at, created_by_user_id FROM api_tokens WHERE token_hash = ?",
		tokenHash,
	).Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID, &permissionsJSON,
		&token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull)

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
	if createdByNull.Valid {
		v := createdByNull.Int64
		token.CreatedByUserID = &v
	}

	return &token, nil
}

// ListAPITokensByUser returns all API tokens for a user
func (d *Database) ListAPITokensByUser(userID int64) ([]*APIToken, error) {
	rows, err := d.query(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id WHERE t.user_id = ? ORDER BY t.created_at DESC",
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
		var createdByNull sql.NullInt64
		var createdByEmail sql.NullString
		var createdByName sql.NullString

		err := rows.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID,
			&permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
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
		if createdByNull.Valid {
			v := createdByNull.Int64
			token.CreatedByUserID = &v
		}
		if createdByEmail.Valid {
			token.CreatedByEmail = createdByEmail.String
		}
		if createdByName.Valid {
			token.CreatedByName = createdByName.String
		}

		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// ListAllAPITokens returns all API tokens with creator metadata
func (d *Database) ListAllAPITokens() ([]*APIToken, error) {
	rows, err := d.query(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id ORDER BY t.created_at DESC",
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
		var createdByNull sql.NullInt64
		var createdByEmail sql.NullString
		var createdByName sql.NullString

		err := rows.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID,
			&permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
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
		if createdByNull.Valid {
			v := createdByNull.Int64
			token.CreatedByUserID = &v
		}
		if createdByEmail.Valid {
			token.CreatedByEmail = createdByEmail.String
		}
		if createdByName.Valid {
			token.CreatedByName = createdByName.String
		}

		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last used timestamp
func (d *Database) UpdateAPITokenLastUsed(tokenHash string) error {
	_, err := d.exec(
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
	_, err := d.exec("DELETE FROM api_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete API token: %w", err)
	}
	return nil
}

// UpdateAPITokenName renames an existing API token
func (d *Database) UpdateAPITokenName(id int64, name string) error {
	_, err := d.exec("UPDATE api_tokens SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("failed to update API token name: %w", err)
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

// CleanupOldAuditLogs deletes audit logs older than retentionDays
func (d *Database) CleanupOldAuditLogs(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	_, err := d.exec("DELETE FROM audit_logs WHERE timestamp < NOW() - (INTERVAL '1 day' * ?::int)", retentionDays)
	return err
}
