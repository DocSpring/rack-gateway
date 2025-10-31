package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	// Blank import required to register pgx driver with database/sql
	_ "github.com/jackc/pgx/v5/stdlib"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Database wraps the SQL database connection
type Database struct {
	db     *sql.DB
	pool   *pgxpool.Pool // For River and other pgx-native operations
	driver string        // always "pgx"
}

// PoolConfig holds database connection pool configuration
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// New creates a new database connection
func New(dsn string) (*Database, error) {
	return NewWithPoolConfig(dsn, nil)
}

// NewWithPoolConfig creates a new database connection with custom pool configuration
// If autoMigrate is true, migrations will be run automatically on startup.
// In production, set autoMigrate to false and run migrations manually using the "migrate" command.
func NewWithPoolConfig(dsn string, poolConfig *PoolConfig) (*Database, error) {
	return NewWithPoolConfigAndMigration(dsn, poolConfig, false)
}

// NewWithPoolConfigAndMigration creates a new database connection with custom pool configuration
// and optionally runs migrations automatically. Set autoMigrate to true only in dev/test environments.
func NewWithPoolConfigAndMigration(dsn string, poolConfig *PoolConfig, autoMigrate bool) (*Database, error) {
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

	// Create database/sql connection (for existing code)
	db, err := sql.Open("pgx", source)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	// Apply connection pool configuration
	applyPoolConfig(db, poolConfig)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	// Create pgxpool connection (for River and pgx-native operations)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, source)
	if err != nil {
		db.Close() //nolint:errcheck // cleanup on init failure
		return nil, fmt.Errorf("failed to create pgxpool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		db.Close() //nolint:errcheck // cleanup on init failure
		return nil, fmt.Errorf("failed to ping pgxpool: %w", err)
	}

	d := &Database{db: db, pool: pool, driver: "pgx"}
	if autoMigrate {
		if err := d.migrateAll(); err != nil {
			pool.Close()
			db.Close() //nolint:errcheck // cleanup on init failure
			return nil, fmt.Errorf("failed to initialize schema: %w", err)
		}
	}
	return d, nil
}

// applyPoolConfig applies connection pool settings to the database connection.
// If poolConfig is nil, defaults from environment variables are used.
func applyPoolConfig(db *sql.DB, poolConfig *PoolConfig) {
	if poolConfig == nil {
		poolConfig = poolConfigFromEnv()
	}

	db.SetMaxOpenConns(poolConfig.MaxOpenConns)
	db.SetMaxIdleConns(poolConfig.MaxIdleConns)
	db.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
	db.SetConnMaxIdleTime(poolConfig.ConnMaxIdleTime)
}

// poolConfigFromEnv returns default pool configuration values.
// Configuration values should be passed in via PoolConfig parameter instead of reading from env.
func poolConfigFromEnv() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	}
}

// NewFromEnv builds a Postgres DSN from env if DATABASE_URL is unset.
// Automatically runs migrations only if DEV_MODE=true (for dev/test environments).
// In production, run migrations manually using the "migrate" command.
func NewFromEnv() (*Database, error) {
	autoMigrate := os.Getenv("DEV_MODE") == "true"

	// A few variations supported to support different Convox resource names
	if dsn := os.Getenv("RGW_DATABASE_URL"); dsn != "" {
		return NewWithPoolConfigAndMigration(dsn, nil, autoMigrate)
	}
	if dsn := os.Getenv("GATEWAY_DATABASE_URL"); dsn != "" {
		return NewWithPoolConfigAndMigration(dsn, nil, autoMigrate)
	}
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return NewWithPoolConfigAndMigration(dsn, nil, autoMigrate)
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
		return NewWithPoolConfigAndMigration(dsn, nil, autoMigrate)
	}
	dsn := fmt.Sprintf("postgres://%s@%s:%s/%s", user, host, port, dbname)
	return NewWithPoolConfigAndMigration(dsn, nil, autoMigrate)
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

func (d *Database) logQuery(prefix, query string, args ...interface{}) {
	logAggregate := gtwlog.TopicEnabled(gtwlog.TopicSQL)
	if !logAggregate {
		return
	}

	compact := strings.Join(strings.Fields(query), " ")
	if len(compact) > 400 {
		compact = compact[:400] + "..."
	}

	color := sqlColor(compact)
	caller := queryCaller()
	argsStr := formatArgs(args)

	segments := make([]string, 0, 3)
	if caller != "" {
		segments = append(segments, fmt.Sprintf("%s%s%s", colorGray, caller, colorReset))
	}
	segments = append(segments, fmt.Sprintf("%s%s%s", colorGray, prefix, colorReset))
	segments = append(segments, fmt.Sprintf("%s%s%s", color, compact, colorReset))

	message := strings.Join(segments, " ") + " args=" + argsStr
	gtwlog.DebugTopicf(gtwlog.TopicSQL, "%s", message)

	logSQLTrace()
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

func formatArgs(args []interface{}) string {
	if len(args) == 0 {
		return "[]"
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%v", arg)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

const (
	colorReset   = "\033[0m"
	colorGray    = "\033[90m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorRed     = "\033[31m"
	colorMagenta = "\033[35m"
)

func sqlColor(query string) string {
	upper := strings.ToUpper(strings.TrimSpace(query))
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		return colorCyan
	case strings.HasPrefix(upper, "INSERT"):
		return colorGreen
	case strings.HasPrefix(upper, "UPDATE"):
		return colorYellow
	case strings.HasPrefix(upper, "DELETE"):
		return colorRed
	default:
		return colorMagenta
	}
}

func queryCaller() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == "" {
			if !more {
				break
			}
			continue
		}
		file := frame.File
		if !strings.Contains(file, "/internal/gateway/db/") && !strings.Contains(file, "\\internal\\gateway\\db\\") {
			rel := relativePath(file)
			return fmt.Sprintf("%s:%d", rel, frame.Line)
		}
		if !more {
			break
		}
	}
	return ""
}

func logSQLTrace() {
	if !gtwlog.TopicEnabled(gtwlog.TopicSQLTrace) {
		return
	}

	lines := collectTraceLines()
	if len(lines) == 0 {
		return
	}

	gtwlog.DebugTopicf(gtwlog.TopicSQLTrace, "%s", strings.Join(lines, "\n"))
}

func collectTraceLines() []string {
	const traceDepth = 10
	pcs := make([]uintptr, 32)
	n := runtime.Callers(4, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	lines := make([]string, 0, traceDepth)
	depth := 0

	for {
		frame, more := frames.Next()
		if frame.Function == "" {
			if !more {
				break
			}
			continue
		}

		if shouldSkipFrame(frame.File) {
			if !more {
				break
			}
			continue
		}

		rel := relativePath(frame.File)
		lines = append(lines, fmt.Sprintf("#%d %s (%s:%d)", depth, frame.Function, rel, frame.Line))
		depth++
		if depth >= traceDepth || !more {
			break
		}
	}

	return lines
}

func shouldSkipFrame(file string) bool {
	// Skip internal database frames so trace points to caller sites
	if strings.Contains(file, "/internal/gateway/db/") || strings.Contains(file, "\\internal\\gateway\\db\\") {
		return true
	}
	// Skip Go runtime frames
	if strings.Contains(file, "/src/runtime/") {
		return true
	}
	return false
}

// ResetDatabase drops all gateway tables and re-applies migrations. It reads
// environment guards from process environment variables so callers do not need
// to pass context explicitly.
func (d *Database) ResetDatabase() error {
	if err := validateResetPermission(); err != nil {
		return err
	}

	devMode := os.Getenv("DEV_MODE") == "true"
	disableEnvCheck := strings.TrimSpace(os.Getenv("DISABLE_DATABASE_ENVIRONMENT_CHECK")) != ""

	if err := d.validateEnvironmentForReset(devMode, disableEnvCheck); err != nil {
		return err
	}

	if err := d.dropAllTables(); err != nil {
		return err
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

func validateResetPermission() error {
	if os.Getenv("RESET_RACK_GATEWAY_DATABASE") != "DELETE_ALL_DATA" {
		return fmt.Errorf(
			"refusing to reset database: set RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA to proceed",
		)
	}
	return nil
}

func (d *Database) validateEnvironmentForReset(devMode, disableEnvCheck bool) error {
	currentEnv, err := d.CurrentEnvironment()
	if err != nil {
		return fmt.Errorf("failed to determine database environment: %w", err)
	}

	switch currentEnv {
	case "":
		if !devMode && !disableEnvCheck {
			return fmt.Errorf(
				"refusing to reset database with unknown environment " +
					"(set DEV_MODE=true for development or DISABLE_DATABASE_ENVIRONMENT_CHECK=1 to override)",
			)
		}
	case "development":
		// always allowed
	default:
		if !disableEnvCheck {
			return fmt.Errorf(
				"refusing to reset %s database without DISABLE_DATABASE_ENVIRONMENT_CHECK",
				currentEnv,
			)
		}
	}

	return nil
}

func (d *Database) dropAllTables() error {
	// Drop audit schema (includes audit_event and audit_event_aggregated tables)
	if _, err := d.exec("DROP SCHEMA IF EXISTS audit CASCADE"); err != nil {
		return fmt.Errorf("failed to drop audit schema: %w", err)
	}

	// Drop dependent tables first to satisfy foreign keys.
	tables := []string{
		"user_resources",
		"deploy_approval_requests",
		"api_tokens",
		"cli_login_states",
		"mfa_backup_codes",
		"mfa_methods",
		"trusted_devices",
		"user_sessions",
		"settings",
		"users",
		"rgw_internal_metadata",
	}

	for _, table := range tables {
		if _, err := d.exec("DROP TABLE IF EXISTS " + table + " CASCADE"); err != nil {
			return fmt.Errorf("failed to drop %s: %w", table, err)
		}
	}

	return nil
}

// Close closes the database connection
func (d *Database) Close() error {
	if d.pool != nil {
		d.pool.Close()
	}
	return d.db.Close()
}

// DB exposes the underlying *sql.DB for tests and advanced usage.
// Avoid using this in application code where higher-level helpers exist.
func (d *Database) DB() *sql.DB {
	return d.db
}

// Pool exposes the underlying *pgxpool.Pool for River and other pgx-native operations.
func (d *Database) Pool() *pgxpool.Pool {
	return d.pool
}

func relativePath(file string) string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Base(file)
	}
	rel, err := filepath.Rel(wd, file)
	if err != nil {
		return filepath.Base(file)
	}
	return rel
}
