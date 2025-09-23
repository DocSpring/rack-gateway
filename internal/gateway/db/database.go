package db

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"net/netip"
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

// SeedConfig carries optional user/email lists to bootstrap the database. Any nil
// slice falls back to environment variables when seeding.
type SeedConfig struct {
	AdminUsers       []string
	ViewerUsers      []string
	DeployerUsers    []string
	OperationsUsers  []string
	ProtectedEnvVars []string
}

// User represents a user in the system
type User struct {
	ID              int64     `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name"`
	Roles           []string  `json:"roles"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Suspended       bool      `json:"suspended"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	CreatedByEmail  string    `json:"created_by_email,omitempty"`
	CreatedByName   string    `json:"created_by_name,omitempty"`
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
	CreatedAt       time.Time  `json:"created_at" ts_type:"string"`
	ExpiresAt       *time.Time `json:"expires_at" ts_type:"string | null"`
	LastUsedAt      *time.Time `json:"last_used_at" ts_type:"string | null"`
}

// RackTLSCert stores the pinned rack TLS certificate information.
type RackTLSCert struct {
	PEM         string    `json:"pem"`
	Fingerprint string    `json:"fingerprint"`
	FetchedAt   time.Time `json:"fetched_at" ts_type:"string"`
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

// UserResource represents a creator->resource mapping
type UserResource struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// New creates a new database connection
func New(dsn string) (*Database, error) {
	// Use provided DSN if it looks like Postgres, else use env var
	source := strings.TrimSpace(dsn)
	if source == "" || !(strings.HasPrefix(strings.ToLower(source), "postgres://") || strings.HasPrefix(strings.ToLower(source), "postgresql://")) {
		// Check CGW_DATABASE_URL first (new Convox automatic env var), then fall back to DATABASE_URL
		source = os.Getenv("CGW_DATABASE_URL")
		if source == "" {
			source = os.Getenv("DATABASE_URL")
		}
	}
	if source == "" {
		return nil, fmt.Errorf("CGW_DATABASE_URL or DATABASE_URL is required")
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
	// A few variations supported to support different Convox resource names
	if dsn := os.Getenv("CGW_DATABASE_URL"); dsn != "" {
		return New(dsn)
	}
	if dsn := os.Getenv("GATEWAY_DATABASE_URL"); dsn != "" {
		return New(dsn)
	}
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

func nullableIP(ip string) interface{} {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.String()
}

// CurrentEnvironment returns the environment string stored in the metadata table, if present.
func (d *Database) CurrentEnvironment() (string, error) {
	row := d.queryRow(`SELECT environment FROM cgw_internal_metadata WHERE id = TRUE`)
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
		INSERT INTO cgw_internal_metadata (id, environment, updated_at)
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

// ResetDatabase drops all gateway tables and re-applies migrations. It reads
// environment guards from process environment variables so callers do not need
// to pass context explicitly.
func (d *Database) ResetDatabase() error {
	if os.Getenv("RESET_CONVOX_GATEWAY_DATABASE") != "DELETE_ALL_DATA" {
		return fmt.Errorf("refusing to reset database: set RESET_CONVOX_GATEWAY_DATABASE=DELETE_ALL_DATA to proceed")
	}
	devMode := os.Getenv("DEV_MODE") == "true"
	disableEnvCheck := strings.TrimSpace(os.Getenv("DISABLE_DATABASE_ENVIRONMENT_CHECK")) != ""

	currentEnv, err := d.CurrentEnvironment()
	if err != nil {
		return fmt.Errorf("failed to determine database environment: %w", err)
	}

	switch currentEnv {
	case "":
		if !devMode && !disableEnvCheck {
			return fmt.Errorf("refusing to reset database with unknown environment (set DEV_MODE=true for development or DISABLE_DATABASE_ENVIRONMENT_CHECK=1 to override)")
		}
	case "development":
		// always allowed
	default:
		if !disableEnvCheck {
			return fmt.Errorf("refusing to reset %s database without DISABLE_DATABASE_ENVIRONMENT_CHECK", currentEnv)
		}
	}

	// Drop dependent tables first to satisfy foreign keys.
	for _, table := range []string{
		"user_resources",
		"api_tokens",
		"audit_logs",
		"cli_login_states",
		"settings",
		"users",
		"cgw_internal_metadata",
	} {
		if _, err := d.exec("DROP TABLE IF EXISTS " + table + " CASCADE"); err != nil {
			return fmt.Errorf("failed to drop %s: %w", table, err)
		}
	}

	if _, err := d.exec("DELETE FROM schema_migrations"); err != nil {
		return fmt.Errorf("failed to reset schema_migrations: %w", err)
	}

	if err := d.migrateAll(); err != nil {
		return fmt.Errorf("failed to re-run migrations: %w", err)
	}

	if err := d.SetEnvironment(devMode); err != nil {
		return err
	}

	if err := d.SeedDatabase(nil); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	return nil
}

// SeedDatabase ensures the gateway database contains the expected bootstrap
// data (admin users, seeded role users, protected env vars). When cfg is nil or
// any slice within is nil, the method falls back to reading the equivalent
// environment variable (e.g., ADMIN_USERS).
func (d *Database) SeedDatabase(cfg *SeedConfig) error {
	seed := buildSeedInputs(cfg)

	if len(seed.adminUsers) > 0 {
		if err := d.InitializeAdmin(seed.adminUsers[0], "Admin User"); err != nil {
			return fmt.Errorf("failed to initialize admin user: %w", err)
		}
	}

	roleSeeds := []struct {
		emails      []string
		defaultName string
		roles       []string
	}{
		{seed.adminUsers, "Admin User", []string{"admin"}},
		{seed.viewerUsers, "Viewer User", []string{"viewer"}},
		{seed.deployerUsers, "Deployer User", []string{"deployer"}},
		{seed.operationsUsers, "Ops User", []string{"ops"}},
	}

	for _, rs := range roleSeeds {
		for _, email := range rs.emails {
			if err := d.ensureUserWithRoles(email, rs.defaultName, rs.roles); err != nil {
				return err
			}
		}
	}

	if len(seed.protectedEnvVars) > 0 {
		raw, ok, err := d.GetSettingRaw("protected_env_vars")
		if err != nil {
			return fmt.Errorf("failed to read protected_env_vars setting: %w", err)
		}
		if !ok || len(raw) == 0 {
			if err := d.UpsertSetting("protected_env_vars", seed.protectedEnvVars, nil); err != nil {
				return fmt.Errorf("failed to seed protected_env_vars: %w", err)
			}
		}
	}

	return nil
}

type seedInputs struct {
	adminUsers       []string
	viewerUsers      []string
	deployerUsers    []string
	operationsUsers  []string
	protectedEnvVars []string
}

func buildSeedInputs(cfg *SeedConfig) seedInputs {
	inputs := seedInputs{}

	if cfg != nil && cfg.AdminUsers != nil {
		inputs.adminUsers = normalizeEmailList(cfg.AdminUsers)
	} else {
		inputs.adminUsers = normalizeEmailList(parseCSVEnv(os.Getenv("ADMIN_USERS")))
	}
	if cfg != nil && cfg.ViewerUsers != nil {
		inputs.viewerUsers = normalizeEmailList(cfg.ViewerUsers)
	} else {
		inputs.viewerUsers = normalizeEmailList(parseCSVEnv(os.Getenv("VIEWER_USERS")))
	}
	if cfg != nil && cfg.DeployerUsers != nil {
		inputs.deployerUsers = normalizeEmailList(cfg.DeployerUsers)
	} else {
		inputs.deployerUsers = normalizeEmailList(parseCSVEnv(os.Getenv("DEPLOYER_USERS")))
	}
	if cfg != nil && cfg.OperationsUsers != nil {
		inputs.operationsUsers = normalizeEmailList(cfg.OperationsUsers)
	} else {
		inputs.operationsUsers = normalizeEmailList(parseCSVEnv(os.Getenv("OPERATIONS_USERS")))
	}

	if cfg != nil && cfg.ProtectedEnvVars != nil {
		inputs.protectedEnvVars = normalizeProtectedEnvVars(cfg.ProtectedEnvVars)
	} else {
		inputs.protectedEnvVars = normalizeProtectedEnvVars(parseCSVEnv(os.Getenv("DB_SEED_PROTECTED_ENV_VARS")))
	}

	return inputs
}

func parseCSVEnv(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func normalizeEmailList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		email := strings.TrimSpace(strings.ToLower(raw))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}

func normalizeProtectedEnvVars(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		key := strings.TrimSpace(strings.ToUpper(raw))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func normalizeRoles(roles []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		role := strings.TrimSpace(strings.ToLower(r))
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (d *Database) ensureUserWithRoles(email, defaultName string, roles []string) error {
	if email == "" {
		return nil
	}

	roles = normalizeRoles(roles)
	existing, err := d.GetUser(email)
	if err != nil {
		return fmt.Errorf("failed to load user %s: %w", email, err)
	}

	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = email
	}

	if existing == nil {
		if _, err := d.CreateUser(email, name, roles); err != nil {
			return fmt.Errorf("failed to create user %s: %w", email, err)
		}
		return nil
	}

	currentRoles := normalizeRoles(existing.Roles)
	if !equalStringSlices(currentRoles, roles) {
		if err := d.UpdateUserRoles(email, roles); err != nil {
			return fmt.Errorf("failed to update roles for %s: %w", email, err)
		}
	}

	if name != "" && name != existing.Name {
		if err := d.UpdateUserName(email, name); err != nil {
			return fmt.Errorf("failed to update name for %s: %w", email, err)
		}
	}

	return nil
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
func (d *Database) deleteUserAuditLogs(email string) error {
	var uid sql.NullInt64
	if err := d.queryRow("SELECT id FROM users WHERE email = ?", email).Scan(&uid); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("failed to load user id for audit cleanup: %w", err)
	}
	if !uid.Valid {
		return nil
	}
	if _, err := d.exec("DELETE FROM audit_logs WHERE user_email = ?", email); err != nil {
		return fmt.Errorf("failed to delete audit logs for user: %w", err)
	}
	return nil
}

func (d *Database) DeleteUser(email string) error {
	if err := d.deleteUserAuditLogs(email); err != nil {
		return err
	}
	_, err := d.exec("DELETE FROM users WHERE email = ?", email)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// ListUsers returns all users
func (d *Database) ListUsers() ([]*User, error) {
	rows, err := d.query(
		`SELECT u.id, u.email, u.name, u.roles, u.created_at, u.updated_at, u.suspended,
			cu.id, cu.email, cu.name
		FROM users u
		LEFT JOIN user_resources ur ON ur.resource_type = 'user' AND ur.resource_id = u.email
		LEFT JOIN users cu ON cu.id = ur.user_id
		ORDER BY u.created_at DESC, u.email`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		var rolesJSON string
		var creatorID sql.NullInt64
		var creatorEmail sql.NullString
		var creatorName sql.NullString

		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Name,
			&rolesJSON,
			&user.CreatedAt,
			&user.UpdatedAt,
			&user.Suspended,
			&creatorID,
			&creatorEmail,
			&creatorName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}

		if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
			return nil, fmt.Errorf("failed to unmarshal roles: %w", err)
		}

		if creatorID.Valid {
			id := creatorID.Int64
			user.CreatedByUserID = &id
		}
		if creatorEmail.Valid {
			user.CreatedByEmail = creatorEmail.String
		}
		if creatorName.Valid {
			user.CreatedByName = creatorName.String
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
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::inet, ?, ?, ?, ?, ?)`,
		log.UserEmail, log.UserName, log.ActionType, log.Action, log.Command, log.Resource, log.ResourceType,
		log.Details, nullableIP(log.IPAddress), log.UserAgent, log.Status, log.RBACDecision, log.HTTPStatus, log.ResponseTimeMs,
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}
	return nil
}

// Settings helpers

// GetSettingRaw returns the raw JSON value for a setting key.
func (d *Database) GetSettingRaw(key string) ([]byte, bool, error) {
	var raw []byte
	err := d.queryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get setting %s: %w", key, err)
	}
	return raw, true, nil
}

// UpsertSetting sets the setting value (as JSON) with optional updated_by_user_id.
func (d *Database) UpsertSetting(key string, value interface{}, updatedByUserID *int64) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal setting %s: %w", key, err)
	}
	if updatedByUserID != nil {
		_, err = d.exec(`INSERT INTO settings (key, value, updated_at, updated_by_user_id)
            VALUES (?, ?::jsonb, NOW(), ?)
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW(), updated_by_user_id = EXCLUDED.updated_by_user_id`, key, string(b), *updatedByUserID)
	} else {
		_, err = d.exec(`INSERT INTO settings (key, value, updated_at)
            VALUES (?, ?::jsonb, NOW())
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, key, string(b))
	}
	if err != nil {
		return fmt.Errorf("failed to upsert setting %s: %w", key, err)
	}
	return nil
}

// GetProtectedEnvVars returns the list of protected env var names (normalized upper-case unique).
func (d *Database) GetProtectedEnvVars() ([]string, error) {
	raw, ok, err := d.GetSettingRaw("protected_env_vars")
	if err != nil || !ok {
		return []string{}, err
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return []string{}, fmt.Errorf("invalid protected_env_vars setting: %w", err)
	}
	// normalize
	seen := map[string]struct{}{}
	out := make([]string, 0, len(arr))
	for _, k := range arr {
		k = strings.TrimSpace(strings.ToUpper(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out, nil
}

// GetAllowDestructiveActions returns whether destructive actions are allowed (default false).
func (d *Database) GetAllowDestructiveActions() (bool, error) {
	raw, ok, err := d.GetSettingRaw("allow_destructive_actions")
	if err != nil || !ok {
		return false, err
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("invalid allow_destructive_actions setting: %w", err)
	}
	return v, nil
}

// GetRackTLSCert returns the pinned rack TLS certificate if it exists.
func (d *Database) GetRackTLSCert() (*RackTLSCert, bool, error) {
	raw, ok, err := d.GetSettingRaw("rack_tls_cert")
	if err != nil || !ok {
		return nil, ok, err
	}
	var cert RackTLSCert
	if err := json.Unmarshal(raw, &cert); err != nil {
		return nil, false, fmt.Errorf("invalid rack_tls_cert setting: %w", err)
	}
	return &cert, true, nil
}

// UpsertRackTLSCert persists the pinned rack TLS certificate.
func (d *Database) UpsertRackTLSCert(cert *RackTLSCert, updatedByUserID *int64) error {
	if cert == nil {
		return fmt.Errorf("rack TLS certificate cannot be nil")
	}
	return d.UpsertSetting("rack_tls_cert", cert, updatedByUserID)
}

// GetAuditLogs retrieves audit logs with optional filters
func (d *Database) GetAuditLogs(userEmail string, since time.Time, limit int) ([]*AuditLog, error) {
	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''), COALESCE("details", ''),
               COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''), "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms"
        FROM "audit_logs"
        WHERE 1=1
    `
	args := []interface{}{}

	if userEmail != "" {
		query += " AND \"user_email\" = ?"
		args = append(args, userEmail)
	}

	if !since.IsZero() {
		query += " AND \"timestamp\" >= ?"
		args = append(args, since.UTC())
	}

	query += " ORDER BY \"timestamp\" DESC"

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

// AuditLogFilters contains all possible filters for audit logs
type AuditLogFilters struct {
	UserEmail    string
	Status       string
	ActionType   string
	ResourceType string
	Search       string
	Since        time.Time
	Until        time.Time
	Limit        int
	Offset       int
}

// GetAuditLogsPaged retrieves audit logs with proper SQL filtering and pagination
func (d *Database) GetAuditLogsPaged(filters AuditLogFilters) ([]*AuditLog, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	// Build WHERE clause with proper SQL filtering
	whereClause := "WHERE 1=1"
	args := []interface{}{}

	if filters.UserEmail != "" {
		whereClause += " AND \"user_email\" = ?"
		args = append(args, filters.UserEmail)
	}
	if filters.Status != "" && filters.Status != "all" {
		whereClause += " AND \"status\" = ?"
		args = append(args, filters.Status)
	}
	if filters.ActionType != "" && filters.ActionType != "all" {
		whereClause += " AND \"action_type\" = ?"
		args = append(args, filters.ActionType)
	}
	if filters.ResourceType != "" && filters.ResourceType != "all" {
		whereClause += " AND \"resource_type\" = ?"
		args = append(args, filters.ResourceType)
	}
	if !filters.Since.IsZero() {
		whereClause += " AND \"timestamp\" >= ?"
		args = append(args, filters.Since.UTC())
	}
	if !filters.Until.IsZero() {
		whereClause += " AND \"timestamp\" <= ?"
		args = append(args, filters.Until.UTC())
	}

	// Full-text search across multiple columns
	if filters.Search != "" {
		whereClause += ` AND (
            "user_email" ILIKE ? OR
            "user_name" ILIKE ? OR
            "action" ILIKE ? OR
            "resource" ILIKE ? OR
            "details" ILIKE ? OR
            host("ip_address"::inet) ILIKE ? OR
            "user_agent" ILIKE ?
        )`
		searchPattern := "%" + filters.Search + "%"
		for i := 0; i < 7; i++ {
			args = append(args, searchPattern)
		}
	}

	// Get total count for pagination - build query safely
	countQuery := "SELECT COUNT(*) FROM \"audit_logs\" " + whereClause
	var total int
	if err := d.queryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Get paginated results - build query safely
	query := `
        SELECT "id", "timestamp", "user_email", COALESCE("user_name", ''), "action_type", "action",
               COALESCE("command", ''), COALESCE("resource", ''), COALESCE("resource_type", ''),
               COALESCE("details", ''), COALESCE(host("ip_address"::inet), ''), COALESCE("user_agent", ''),
               "status", COALESCE("rbac_decision", ''), COALESCE("http_status", 0), "response_time_ms"
        FROM "audit_logs" ` + whereClause + `
        ORDER BY "timestamp" DESC
		LIMIT ? OFFSET ?`

	args = append(args, filters.Limit, filters.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var log AuditLog
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.UserEmail, &log.UserName, &log.ActionType, &log.Action, &log.Command, &log.Resource, &log.ResourceType, &log.Details, &log.IPAddress, &log.UserAgent, &log.Status, &log.RBACDecision, &log.HTTPStatus, &log.ResponseTimeMs); err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, &log)
	}
	return logs, total, nil
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

// GetAPITokenByID retrieves an API token by ID
func (d *Database) GetAPITokenByID(id int64) (*APIToken, error) {
	var token APIToken
	var permissionsJSON string
	var expiresAtNull sql.NullTime
	var lastUsedAtNull sql.NullTime
	var createdByNull sql.NullInt64
	var createdByEmail sql.NullString
	var createdByName sql.NullString

	row := d.queryRow(
		"SELECT t.id, t.token_hash, t.name, t.user_id, t.permissions, t.created_at, t.expires_at, t.last_used_at, t.created_by_user_id, cu.email, cu.name FROM api_tokens t LEFT JOIN users cu ON cu.id = t.created_by_user_id WHERE t.id = ?",
		id,
	)
	err := row.Scan(&token.ID, &token.TokenHash, &token.Name, &token.UserID, &permissionsJSON, &token.CreatedAt, &expiresAtNull, &lastUsedAtNull, &createdByNull, &createdByEmail, &createdByName)
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
		v := expiresAtNull.Time
		token.ExpiresAt = &v
	}
	if lastUsedAtNull.Valid {
		v := lastUsedAtNull.Time
		token.LastUsedAt = &v
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

// UpdateAPITokenPermissions replaces the permission set for an API token
func (d *Database) UpdateAPITokenPermissions(id int64, permissions []string) error {
	permsJSON, err := json.Marshal(permissions)
	if err != nil {
		return fmt.Errorf("failed to marshal permissions: %w", err)
	}
	_, err = d.exec("UPDATE api_tokens SET permissions = ? WHERE id = ?", string(permsJSON), id)
	if err != nil {
		return fmt.Errorf("failed to update API token permissions: %w", err)
	}
	return nil
}

// UpdateUserName updates a user's display name by email
func (d *Database) UpdateUserName(email, name string) error {
	_, err := d.exec("UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?", name, email)
	if err != nil {
		return fmt.Errorf("failed to update user name: %w", err)
	}
	return nil
}

// UpdateUserEmail updates a user's email address
func (d *Database) UpdateUserEmail(oldEmail, newEmail string) error {
	_, err := d.exec("UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?", newEmail, oldEmail)
	if err != nil {
		return fmt.Errorf("failed to update user email: %w", err)
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

// CreateUserResource records the creator for a resource. Upserts on (resource_type, resource_id).
func (d *Database) CreateUserResource(userID int64, resourceType, resourceID string) (bool, error) {
	if strings.TrimSpace(resourceType) == "" || strings.TrimSpace(resourceID) == "" {
		return false, fmt.Errorf("invalid resource: type and id required")
	}
	res, err := d.exec(`
        INSERT INTO user_resources (user_id, resource_type, resource_id)
        VALUES (?, ?, ?)
        ON CONFLICT (resource_type, resource_id) DO NOTHING
    `, userID, resourceType, resourceID)
	if err != nil {
		return false, fmt.Errorf("failed to create user_resource: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect user_resource insert: %w", err)
	}
	return rows > 0, nil
}

// GetResourceCreator returns the user_id for a given resource if present.
func (d *Database) GetResourceCreator(resourceType, resourceID string) (int64, bool, error) {
	var uid int64
	err := d.queryRow(`SELECT user_id FROM user_resources WHERE resource_type = ? AND resource_id = ?`, resourceType, resourceID).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to query user_resource: %w", err)
	}
	return uid, true, nil
}

type CreatorInfo struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// GetResourceCreators returns a map of resource_id -> creator info for the given IDs.
func (d *Database) GetResourceCreators(resourceType string, ids []string) (map[string]*CreatorInfo, error) {
	out := make(map[string]*CreatorInfo)
	if len(ids) == 0 {
		return out, nil
	}
	// Build IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, resourceType)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `
        SELECT ur.resource_id, u.id, u.email, u.name
        FROM user_resources ur
        JOIN users u ON u.id = ur.user_id
        WHERE ur.resource_type = ? AND ur.resource_id IN (` + strings.Join(placeholders, ",") + `)
    `
	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query creators: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rid string
		var uid int64
		var email, name string
		if err := rows.Scan(&rid, &uid, &email, &name); err != nil {
			return nil, fmt.Errorf("failed to scan creators: %w", err)
		}
		out[rid] = &CreatorInfo{UserID: uid, Email: email, Name: name}
	}
	return out, nil
}
