package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

)

type Config struct {
	Port                  string
	JWTSecret             string
	JWTExpiry             time.Duration
	GoogleClientID        string
	GoogleClientSecret    string
	GoogleAllowedDomain   string
	RedirectURL           string
	AdminUsers            []string
	UsersConfigPath       string
	RolesConfigPath       string
	PoliciesPath          string
	DevMode               bool
	Racks                 map[string]RackConfig
}

type RackConfig struct {
	Name    string
	URL     string
	APIKey  string
	Region  string
	Enabled bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		JWTExpiry:           30 * 24 * time.Hour,
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleAllowedDomain: getEnv("GOOGLE_ALLOWED_DOMAIN", "docspring.com"),
		RedirectURL:         getEnv("REDIRECT_URL", "http://localhost:8080/v1/login/callback"),
		UsersConfigPath:     getEnv("USERS_CONFIG_PATH", "config/users.yaml"),
		RolesConfigPath:     getEnv("ROLES_CONFIG_PATH", "config/roles.yaml"),
		PoliciesPath:        getEnv("POLICIES_PATH", "config/policies.yaml"),
		DevMode:             getEnv("DEV_MODE", "false") == "true",
		Racks:               make(map[string]RackConfig),
	}

	jwtKey := getEnv("APP_JWT_KEY", "")
	if jwtKey == "" {
		if cfg.DevMode {
			jwtKey = generateDevKey()
			fmt.Printf("Generated dev JWT key: %s\n", jwtKey)
		} else {
			return nil, fmt.Errorf("APP_JWT_KEY is required in production")
		}
	}
	cfg.JWTSecret = jwtKey

	adminUsers := getEnv("ADMIN_USERS", "")
	if adminUsers != "" {
		cfg.AdminUsers = strings.Split(adminUsers, ",")
	}

	cfg.loadRacksFromEnv()

	return cfg, nil
}

func (c *Config) loadRacksFromEnv() {
	// Load the single rack this gateway is protecting
	rackURL := os.Getenv("RACK_URL")
	rackToken := os.Getenv("RACK_TOKEN")
	
	if rackURL != "" && rackToken != "" {
		// The gateway protects a single rack
		c.Racks["default"] = RackConfig{
			Name:    "default",
			URL:     rackURL,
			APIKey:  rackToken,
			Enabled: true,
		}
	} else if c.DevMode {
		// In dev mode, set up a default local rack if none configured
		c.setupDevRacks()
	}
}

func (c *Config) setupDevRacks() {
	c.Racks["local"] = RackConfig{
		Name:    "local",
		URL:     "http://localhost:5443",
		APIKey:  "dev-token",
		Region:  "us-east-1",
		Enabled: true,
	}
}

func generateDevKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}