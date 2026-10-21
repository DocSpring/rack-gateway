package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	// Blank import required to register pgx driver with database/sql
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// Database wraps the SQL database connection
type Database struct {
	db     *sql.DB
	pool   *pgxpool.Pool    // For River and other pgx-native operations
	driver string           // always "pgx"
	dbName string           // database name for lock uniqueness
	cipher *colcrypt.Cipher // encrypts secret columns (see column_encryption.go)
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
// COLUMN_ENCRYPTION_KEY must be set: every command that opens the database (server, migrate, reset-db,
// write-anchor) refuses to run without a valid key, so a missing key fails a deploy before it starts.
func NewWithPoolConfigAndMigration(dsn string, poolConfig *PoolConfig, autoMigrate bool) (*Database, error) {
	columnCipher, err := colcrypt.New(os.Getenv(colcrypt.EnvVar))
	if err != nil {
		return nil, err
	}
	return openDatabase(dsn, poolConfig, autoMigrate, columnCipher)
}

func openDatabase(
	dsn string,
	poolConfig *PoolConfig,
	autoMigrate bool,
	columnCipher *colcrypt.Cipher,
) (*Database, error) {
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
	if poolConfig == nil {
		poolConfig = poolConfigFromEnv()
	}
	applyPoolConfig(db, poolConfig)

	if err := db.Ping(); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to ping postgres: %w", err), db.Close())
	}

	// Create pgxpool connection (for River and pgx-native operations)
	pool, err := openPgxPool(source, poolConfig)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}

	// Extract database name from DSN for unique migration locks
	dbName := extractDatabaseName(source)

	d := &Database{db: db, pool: pool, driver: "pgx", dbName: dbName, cipher: columnCipher}
	if autoMigrate {
		if err := d.migrateAll(); err != nil {
			return nil, errors.Join(fmt.Errorf("failed to initialize schema: %w", err), d.Close())
		}
	}
	return d, nil
}

// openPgxPool opens a pgx pool with the same limits as the database/sql pool.
func openPgxPool(source string, poolConfig *PoolConfig) (*pgxpool.Pool, error) {
	pgxConfig, err := pgxpool.ParseConfig(source)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgxpool config: %w", err)
	}
	if pgxConfig.MaxConns, err = pgxPoolSize("DB_MAX_OPEN_CONNS", poolConfig.MaxOpenConns); err != nil {
		return nil, err
	}
	if pgxConfig.MinConns, err = pgxPoolSize("DB_MAX_IDLE_CONNS", poolConfig.MaxIdleConns); err != nil {
		return nil, err
	}
	pgxConfig.MaxConnLifetime = poolConfig.ConnMaxLifetime
	pgxConfig.MaxConnIdleTime = poolConfig.ConnMaxIdleTime

	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, pgxConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping pgxpool: %w", err)
	}
	return pool, nil
}

func pgxPoolSize(name string, size int) (int32, error) {
	if size < 0 || size > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be between 0 and %d, got %d", name, math.MaxInt32, size)
	}
	return int32(size), nil
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

// Default pool sizes. Each process has two pools of this size (database/sql for requests, audit writes and the
// anchor lock; pgx for River), so it can open up to twice defaultMaxOpenConns connections. A request holds one for
// milliseconds and the hourly anchor write holds one for at most River's one-minute job timeout, so 10 leaves
// room for audit writes (which give up after 10 seconds, including the wait for a connection) while staying
// well under the connection limit of a small Postgres instance during a rolling deploy.
const (
	defaultMaxOpenConns = 10
	defaultMaxIdleConns = 3
)

// poolConfigFromEnv returns pool configuration values from environment variables, or the defaults.
func poolConfigFromEnv() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    envInt("DB_MAX_OPEN_CONNS", defaultMaxOpenConns),
		MaxIdleConns:    envInt("DB_MAX_IDLE_CONNS", defaultMaxIdleConns),
		ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		ConnMaxIdleTime: envDuration("DB_CONN_MAX_IDLE_TIME", 10*time.Minute),
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// NewFromEnv builds a Postgres DSN from env if DATABASE_URL is unset.
// Automatically runs migrations only in dev mode (a testbypass build with DEV_MODE=true).
// In production, run migrations manually using the "migrate" command.
func NewFromEnv() (*Database, error) {
	autoMigrate := testbypass.DevMode()

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

// ensureSSLMode sets the DSN's sslmode to PGSSLMODE when that's set, and otherwise leaves the DSN alone.
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
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func (_ *Database) logQuery(prefix, query string, args ...interface{}) {
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

// ResetDatabase drops all gateway tables and re-applies migrations. It reads
// environment guards from process environment variables so callers do not need
// to pass context explicitly.
func (d *Database) ResetDatabase() error {
	if err := validateResetPermission(); err != nil {
		return err
	}

	devMode := testbypass.DevMode()
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

	// The gateway seeds its users (audit-logged) when it next starts.
	return d.SetEnvironment(devMode)
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
					"(use DEV_MODE=true in a testbypass build or DISABLE_DATABASE_ENVIRONMENT_CHECK=1 to override)",
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
		"webauthn_challenges",
		"audit_verification_status",
		"user_resources",
		"rack_object_keys",
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

// extractDatabaseName extracts the database name from a postgres:// DSN
func extractDatabaseName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	// Path is /dbname, so trim the leading slash
	return strings.TrimPrefix(u.Path, "/")
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
