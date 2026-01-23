// totp-gen generates TOTP codes for testing.
// Usage:
//
//	totp-gen <base32-secret>
//	totp-gen --db [email-filter]
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"
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
	case "--help", "-h":
		printUsage()
		return nil
	default:
		return generateFromSecret(arg)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  totp-gen <base32-secret>    Generate TOTP code from secret")
	fmt.Fprintln(os.Stderr, "  totp-gen --db [email]       Generate TOTP code from dev database")
	fmt.Fprintln(os.Stderr, "  totp-gen --help             Show this help")
}

func generateFromSecret(secret string) error {
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return fmt.Errorf("generating TOTP: %w", err)
	}
	fmt.Println(code)
	return nil
}

func generateFromDB(emailFilter string) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set (mise.toml defines it for dev)")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	var query string
	var args []interface{}

	if emailFilter != "" {
		query = `SELECT u.email, m.secret FROM mfa_methods m
                 JOIN users u ON m.user_id = u.id
                 WHERE m.type = 'totp' AND m.secret IS NOT NULL
                 AND u.email ILIKE $1 LIMIT 1`
		args = []interface{}{"%" + emailFilter + "%"}
	} else {
		query = `SELECT u.email, m.secret FROM mfa_methods m
                 JOIN users u ON m.user_id = u.id
                 WHERE m.type = 'totp' AND m.secret IS NOT NULL LIMIT 1`
	}

	var email, secret string
	err = db.QueryRowContext(ctx, query, args...).Scan(&email, &secret)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no TOTP secrets found in dev database - set up TOTP MFA for a user first")
	} else if err != nil {
		return fmt.Errorf("querying database: %w", err)
	}

	secret = strings.TrimSpace(secret)
	fmt.Fprintf(os.Stderr, "User: %s\n", email)

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return fmt.Errorf("generating TOTP: %w", err)
	}
	fmt.Println(code)
	return nil
}
