package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// New creates a new database connection
func New(dsn string) (*Database, error) {
	// Use provided DSN if it looks like Postgres, else use env var
	source := strings.TrimSpace(dsn)
	lower := strings.ToLower(source)
	if source == "" || (!strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://")) {
		// Check RGW_DATABASE_URL first (new Convox automatic env var), then fall back to DATABASE_URL
		source = os.Getenv("RGW_DATABASE_URL")
		if source == "" {
			source = os.Getenv("DATABASE_URL")
		}
	}
	if source == "" {
		return nil, fmt.Errorf("RGW_DATABASE_URL or DATABASE_URL is required")
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
	logSQL := os.Getenv("LOG_SQL_QUERIES") == "true"
	d := &Database{db: db, driver: "pgx", logSQL: logSQL}
	if err := d.migrateAll(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return d, nil
}

// NewFromEnv builds a Postgres DSN from env if DATABASE_URL is unset.
func NewFromEnv() (*Database, error) {
	// A few variations supported to support different Convox resource names
	if dsn := os.Getenv("RGW_DATABASE_URL"); dsn != "" {
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

const (
	colorReset   = "\033[0m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorRed     = "\033[31m"
	colorMagenta = "\033[35m"
	colorGray    = "\033[90m"
)

func sqlColor(query string) string {
	q := strings.TrimSpace(strings.ToUpper(query))
	switch {
	case strings.HasPrefix(q, "SELECT"):
		return colorCyan
	case strings.HasPrefix(q, "INSERT"):
		return colorGreen
	case strings.HasPrefix(q, "UPDATE"):
		return colorYellow
	case strings.HasPrefix(q, "DELETE"):
		return colorRed
	default:
		return colorMagenta
	}
}

func (d *Database) logQuery(prefix, query string, args ...interface{}) {
	if !d.logSQL {
		return
	}
	color := sqlColor(query)
	// Compact query for logging (single line, trimmed whitespace)
	q := strings.Join(strings.Fields(query), " ")
	if len(q) > 400 {
		q = q[:400] + "..."
	}
	// Format args with commas
	argsStr := "[]"
	if len(args) > 0 {
		argStrs := make([]string, len(args))
		for i, arg := range args {
			argStrs[i] = fmt.Sprintf("%v", arg)
		}
		argsStr = "[" + strings.Join(argStrs, ", ") + "]"
	}
	fmt.Printf("%s%s%s %s%s%s %s%s%s\n",
		colorGray, prefix, colorReset,
		color, q, colorReset,
		colorGray, argsStr, colorReset,
	)
}

func (d *Database) exec(q string, args ...interface{}) (sql.Result, error) {
	d.logQuery("EXEC:", q, args...)
	return d.db.Exec(d.rebind(q), args...)
}

func (d *Database) execTx(tx *sql.Tx, q string, args ...interface{}) (sql.Result, error) {
	d.logQuery("EXEC (TX):", q, args...)
	return tx.Exec(d.rebind(q), args...)
}

func (d *Database) query(q string, args ...interface{}) (*sql.Rows, error) {
	d.logQuery("QUERY:", q, args...)
	return d.db.Query(d.rebind(q), args...)
}

func (d *Database) queryRow(q string, args ...interface{}) *sql.Row {
	d.logQuery("QUERY ROW:", q, args...)
	return d.db.QueryRow(d.rebind(q), args...)
}

// ResetDatabase drops all gateway tables and re-applies migrations. It reads
// environment guards from process environment variables so callers do not need
// to pass context explicitly.
func (d *Database) ResetDatabase() error {
	if os.Getenv("RESET_RACK_GATEWAY_DATABASE") != "DELETE_ALL_DATA" {
		return fmt.Errorf("refusing to reset database: set RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA to proceed")
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
		"deploy_approval_requests",
		"api_tokens",
		"audit_logs",
		"cli_login_states",
		"mfa_backup_codes",
		"mfa_methods",
		"trusted_devices",
		"user_sessions",
		"settings",
		"users",
		"rgw_internal_metadata",
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

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// DB exposes the underlying *sql.DB for tests and advanced usage.
// Avoid using this in application code where higher-level helpers exist.
func (d *Database) DB() *sql.DB {
	return d.db
}
