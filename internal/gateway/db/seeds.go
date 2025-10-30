package db

import (
	"encoding/json"
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

	// Settings are no longer pre-seeded into the database.
	// They are read from environment variables by the settings service on demand.

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
	query := "INSERT INTO users (email, name, roles) VALUES (?, ?, ?) RETURNING id"
	if err := d.queryRow(query, email, name, string(rolesJSON)).Scan(&id); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}
	return nil
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
