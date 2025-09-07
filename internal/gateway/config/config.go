package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	JWTSecret           string
	JWTExpiry           time.Duration
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleAllowedDomain string
	GoogleOAuthBaseURL  string
	RedirectURL         string
	AdminUsers          []string
	DevMode             bool
	Racks               map[string]RackConfig
	LogResponseBodies   bool
	LogResponseMaxBytes int
}

type RackConfig struct {
	Name     string
	URL      string
	Username string // Username for Basic Auth (default: "convox")
	APIKey   string // Password for Basic Auth
	Region   string
	Enabled  bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("GATEWAY_PORT", "8080"),
		JWTExpiry:           30 * 24 * time.Hour,
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleAllowedDomain: getEnv("GOOGLE_ALLOWED_DOMAIN", ""),
		GoogleOAuthBaseURL:  getEnv("GOOGLE_OAUTH_BASE_URL", ""),
		RedirectURL:         getEnv("REDIRECT_URL", ""),
		DevMode:             getEnv("DEV_MODE", "false") == "true",
		Racks:               make(map[string]RackConfig),
		LogResponseBodies:   getEnv("GATEWAY_LOG_RESPONSE_BODIES", "false") == "true",
		LogResponseMaxBytes: 16384,
	}
	if mb := getEnv("GATEWAY_LOG_RESPONSE_MAX_BYTES", "65536"); mb != "" {
		if v, err := strconv.Atoi(mb); err == nil && v > 0 {
			cfg.LogResponseMaxBytes = v
		}
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
	rackHost := os.Getenv("RACK_HOST")
	rackToken := os.Getenv("RACK_TOKEN")
	rackUsername := getEnv("RACK_USERNAME", "convox") // Default to "convox" for standard Convox racks

	if rackHost != "" && rackToken != "" {
		// Add https:// if no protocol specified
		if !strings.HasPrefix(rackHost, "http://") && !strings.HasPrefix(rackHost, "https://") {
			rackHost = "https://" + rackHost
		}

		// The gateway protects a single rack
		c.Racks["default"] = RackConfig{
			Name:     "default",
			URL:      rackHost,
			Username: rackUsername,
			APIKey:   rackToken,
			Enabled:  true,
		}
	} else if c.DevMode {
		// In dev mode, set up a default local rack if none configured
		c.setupDevRacks()
	}
}

func (c *Config) setupDevRacks() {
	c.Racks["local"] = RackConfig{
		Name:     "local",
		URL:      "http://localhost:5443",
		Username: "convox",
		APIKey:   "dev-token",
		Region:   "us-east-1",
		Enabled:  true,
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
