package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// SeedConfig carries optional user/email lists to bootstrap the database. Any nil
// slice falls back to environment variables when seeding.
type SeedConfig struct {
	AdminUsers      []string
	ViewerUsers     []string
	DeployerUsers   []string
	OperationsUsers []string
}

// SeededUser is a user the startup seed created, with the roles of every seed list that names it.
type SeededUser struct {
	Email string
	Roles []string
}

// SeedDatabase creates the users in the seed lists that don't exist yet, each with the roles of the lists that name
// it. It never changes an existing user, so a role an admin has changed since stays changed, and seeded users have
// no name until they first sign in, when it's filled from the identity provider. When cfg is nil or any slice
// within is nil, that list is read from its environment variable (e.g., ADMIN_USERS).
//
// Each user's audit event, entry(u), is appended in the same transaction, so a user is never created without its
// event: when an append fails, nothing is created and the error wraps ErrAuditAppend. It returns the stored events.
func (d *Database) SeedDatabase(cfg *SeedConfig, entry func(SeededUser) *AuditLog) ([]*AuditLog, error) {
	users := seedUsers(buildSeedInputs(cfg))
	if len(users) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	tx, err := d.beginAuditTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The chain lock first, as every audit append takes it before touching the rows its event references.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockAuditChain); err != nil {
		return nil, fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}
	var events []*AuditLog
	for _, user := range users {
		created, err := d.insertSeededUser(ctx, tx, user)
		if err != nil {
			return nil, err
		}
		if !created {
			continue
		}
		event := entry(user)
		if err := appendAuditLogTx(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("%w for seeded user %s: %w", ErrAuditAppend, user.Email, err)
		}
		events = append(events, event)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit seeded users: %w", err)
	}
	return events, nil
}

// insertSeededUser creates the user unless one with that email exists, and reports whether it did.
func (d *Database) insertSeededUser(ctx context.Context, tx *sql.Tx, user SeededUser) (bool, error) {
	rolesJSON, err := json.Marshal(user.Roles)
	if err != nil {
		return false, fmt.Errorf("failed to marshal roles: %w", err)
	}
	var id int64
	err = tx.QueryRowContext(ctx, d.rebind(
		"INSERT INTO users (email, name, roles) VALUES (?, '', ?) ON CONFLICT (email) DO NOTHING RETURNING id"),
		user.Email, string(rolesJSON)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to create seeded user %s: %w", user.Email, err)
	}
	return true, nil
}

// seedUsers merges the seed lists into one user per email, sorted by email.
func seedUsers(inputs seedInputs) []SeededUser {
	lists := []struct {
		emails []string
		role   string
	}{
		{inputs.adminUsers, "admin"},
		{inputs.viewerUsers, "viewer"},
		{inputs.deployerUsers, "deployer"},
		{inputs.operationsUsers, "ops"},
	}
	roles := map[string][]string{}
	for _, list := range lists {
		for _, email := range list.emails {
			roles[email] = append(roles[email], list.role)
		}
	}
	users := make([]SeededUser, 0, len(roles))
	for email, userRoles := range roles {
		users = append(users, SeededUser{Email: email, Roles: normalizeRoles(userRoles)})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	return users
}

type seedInputs struct {
	adminUsers      []string
	viewerUsers     []string
	deployerUsers   []string
	operationsUsers []string
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
