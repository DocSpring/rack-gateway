// totp-gen generates TOTP codes for development and tests.
// Usage:
//
//	totp-gen <base32-secret>
//	totp-gen --db [email-filter]
//	totp-gen encrypt --user-id <id> <base32-secret>
//
// TOTP seeds are stored encrypted (see internal/gateway/colcrypt), so --db and encrypt need the gateway's
// COLUMN_ENCRYPTION_KEY (mise.toml sets the development key).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return errors.New("missing argument")
	}

	arg := os.Args[1]

	switch arg {
	case "--db":
		emailFilter := ""
		if len(os.Args) > 2 {
			emailFilter = os.Args[2]
		}
		return generateFromDB(emailFilter)
	case "encrypt":
		return encryptSecret(os.Args[2:])
	case "--help", "-h":
		printUsage()
		return nil
	default:
		return generateFromSecret(arg)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  totp-gen <base32-secret>                     Generate TOTP code from secret")
	fmt.Fprintln(os.Stderr, "  totp-gen --db [email]                        Generate TOTP code from dev database")
	fmt.Fprintln(os.Stderr, "  totp-gen encrypt --user-id <id> <secret>     Print the secret as stored in mfa_methods")
	fmt.Fprintln(os.Stderr, "  totp-gen --help                              Show this help")
}

func generateFromSecret(secret string) error {
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return fmt.Errorf("generating TOTP: %w", err)
	}
	fmt.Println(code)
	return nil
}

// encryptSecret prints a TOTP seed encrypted for a user's mfa_methods row, for test scripts that insert
// MFA methods directly.
func encryptSecret(args []string) error {
	flags := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	userID := flags.Int64("user-id", 0, "id of the user the secret belongs to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *userID <= 0 || flags.NArg() != 1 {
		return errors.New("usage: totp-gen encrypt --user-id <id> <base32-secret>")
	}

	cipher, err := colcrypt.New(os.Getenv(colcrypt.EnvVar))
	if err != nil {
		return err
	}
	envelope, err := cipher.Encrypt(strings.TrimSpace(flags.Arg(0)), colcrypt.MFASecretScope(*userID))
	if err != nil {
		return err
	}
	fmt.Println(envelope)
	return nil
}

func generateFromDB(emailFilter string) error {
	cipher, err := colcrypt.New(os.Getenv(colcrypt.EnvVar))
	if err != nil {
		return err
	}

	stored, err := loadTOTPSecret(emailFilter)
	if err != nil {
		return err
	}
	secret, err := cipher.Decrypt(stored.value, colcrypt.MFASecretScope(stored.userID))
	if err != nil {
		return fmt.Errorf("decrypting the TOTP secret for %s: %w", stored.email, err)
	}
	fmt.Fprintf(os.Stderr, "User: %s\n", stored.email)

	code, err := totp.GenerateCode(strings.TrimSpace(secret), time.Now())
	if err != nil {
		return fmt.Errorf("generating TOTP: %w", err)
	}
	fmt.Println(code)
	return nil
}

type storedSecret struct {
	email  string
	userID int64
	value  string // encrypted
}

// loadTOTPSecret returns the newest TOTP method whose user's email matches emailFilter.
func loadTOTPSecret(emailFilter string) (*storedSecret, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, errors.New("DATABASE_URL is not set (mise.toml defines it for dev)")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `SELECT u.email, m.user_id, m.secret FROM mfa_methods m
              JOIN users u ON m.user_id = u.id
              WHERE m.type = 'totp' AND m.secret IS NOT NULL
              AND ($1 = '' OR u.email ILIKE '%' || $1 || '%')
              ORDER BY m.created_at DESC LIMIT 1`

	var stored storedSecret
	err = db.QueryRowContext(ctx, query, emailFilter).Scan(&stored.email, &stored.userID, &stored.value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, errors.New("no TOTP secrets found in dev database - set up TOTP MFA for a user first")
	case err != nil:
		return nil, fmt.Errorf("querying database: %w", err)
	}
	return &stored, nil
}
