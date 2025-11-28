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

// Config holds all application configuration settings loaded from environment variables.
type Config struct {
	Port                  string
	Domain                string
	SentryDSN             string
	SentryEnvironment     string
	SentryRelease         string
	SentryJSDsn           string
	SentryJSTracesRate    string
	SentryTestsEnabled    bool
	SessionSecret         string
	GoogleClientID        string
	GoogleClientSecret    string
	GoogleAllowedDomain   string
	GoogleOAuthBaseURL    string
	SlackClientID         string
	SlackClientSecret     string
	AdminUsers            []string
	ViewerUsers           []string
	DeployerUsers         []string
	OperationsUsers       []string
	DevMode               bool
	Racks                 map[string]RackConfig
	RackTLSPinningEnabled bool
	TrustedProxies        []string
	GitHubToken           string
	CircleCIToken         string
	DBMaxOpenConns        int
	DBMaxIdleConns        int
	DBConnMaxLifetime     time.Duration
	DBConnMaxIdleTime     time.Duration
}

// RackConfig holds configuration for a single Convox rack connection.
type RackConfig struct {
	Name        string
	Alias       string
	DisplayName string // Human-readable name for notifications (e.g., "Staging", "US Production")
	URL         string
	Username    string // Username for Basic Auth (default: "convox")
	APIKey      string // Password for Basic Auth
	Region      string
	Enabled     bool
}

var randRead = rand.Read

func normalizeSampleRate(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fallback
}

// Load reads all configuration from environment variables and returns a Config instance.
func Load() (*Config, error) {
	cfg := loadBaseConfig()

	if err := cfg.loadSessionSecret(); err != nil {
		return nil, err
	}

	cfg.loadUserRoles()
	cfg.loadTrustedProxies()
	cfg.loadRacksFromEnv()
	cfg.loadIntegrationTokens()
	cfg.loadDatabasePoolConfig()

	return cfg, nil
}

func loadBaseConfig() *Config {
	release := strings.TrimSpace(getEnv("SENTRY_RELEASE", ""))
	if release == "" {
		release = strings.TrimSpace(os.Getenv("RELEASE"))
	}

	jsTracesRate := normalizeSampleRate(getEnv("SENTRY_JS_TRACES_SAMPLE_RATE", ""), "0")

	return &Config{
		Port:                getEnv("PORT", "8080"),
		Domain:              getEnv("DOMAIN", ""),
		SentryDSN:           strings.TrimSpace(getEnv("SENTRY_DSN", "")),
		SentryEnvironment:   strings.TrimSpace(getEnv("SENTRY_ENVIRONMENT", "")),
		SentryRelease:       release,
		SentryJSDsn:         strings.TrimSpace(getEnv("SENTRY_JS_DSN", "")),
		SentryJSTracesRate:  jsTracesRate,
		SentryTestsEnabled:  getEnv("ENABLE_SENTRY_TEST_BUTTONS", "false") == "true",
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleAllowedDomain: getEnv("GOOGLE_ALLOWED_DOMAIN", ""),
		GoogleOAuthBaseURL:  getEnv("GOOGLE_OAUTH_BASE_URL", ""),
		SlackClientID:       getEnv("SLACK_CLIENT_ID", ""),
		SlackClientSecret:   getEnv("SLACK_CLIENT_SECRET", ""),
		DevMode:             getEnv("DEV_MODE", "false") == "true",
		Racks:               make(map[string]RackConfig),

		// Disabled by default because the Convox rack API currently generates a fresh self-signed
		// certificate on every restart (see stdapi.Server.Listen). Pinning that dynamic cert would
		// break after each deploy. If Convox supports providing a stable internal certificate in the
		// future, operators can enable this flag to re-activate TOFU pinning.
		RackTLSPinningEnabled: getEnv("ENABLE_RACK_TLS_PINNING", "false") == "true",
	}
}

func (c *Config) loadSessionSecret() error {
	sessionSecret := getEnv("APP_SECRET_KEY", "")
	if sessionSecret == "" {
		if !c.DevMode {
			return fmt.Errorf("APP_SECRET_KEY is required in production")
		}
		var err error
		sessionSecret, err = generateDevKey()
		if err != nil {
			return fmt.Errorf("failed to generate dev secret key: %w", err)
		}
	}
	c.SessionSecret = sessionSecret
	return nil
}

func (c *Config) loadUserRoles() {
	if adminUsers := getEnv("ADMIN_USERS", ""); adminUsers != "" {
		c.AdminUsers = strings.Split(adminUsers, ",")
	}
	if viewerUsers := getEnv("VIEWER_USERS", ""); viewerUsers != "" {
		c.ViewerUsers = strings.Split(viewerUsers, ",")
	}
	if deployerUsers := getEnv("DEPLOYER_USERS", ""); deployerUsers != "" {
		c.DeployerUsers = strings.Split(deployerUsers, ",")
	}
	if operationsUsers := getEnv("OPERATIONS_USERS", ""); operationsUsers != "" {
		c.OperationsUsers = strings.Split(operationsUsers, ",")
	}
}

func (c *Config) loadTrustedProxies() {
	proxies := strings.TrimSpace(getEnv("TRUSTED_PROXY_CIDRS", ""))
	if proxies == "" {
		return
	}
	for _, entry := range strings.Split(proxies, ",") {
		entry = strings.TrimSpace(entry)
		if entry != "" {
			c.TrustedProxies = append(c.TrustedProxies, entry)
		}
	}
}

func (c *Config) loadIntegrationTokens() {
	c.GitHubToken = strings.TrimSpace(getEnv("GITHUB_TOKEN", ""))
	c.CircleCIToken = strings.TrimSpace(getEnv("CIRCLECI_TOKEN", ""))
}

func (c *Config) loadDatabasePoolConfig() {
	c.DBMaxOpenConns = getEnvInt("DB_MAX_OPEN_CONNS", 25)
	c.DBMaxIdleConns = getEnvInt("DB_MAX_IDLE_CONNS", 5)
	c.DBConnMaxLifetime = getEnvDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute)
	c.DBConnMaxIdleTime = getEnvDuration("DB_CONN_MAX_IDLE_TIME", 10*time.Minute)
}

func (c *Config) loadRacksFromEnv() {
	rackHost := os.Getenv("RACK_HOST")
	rackToken := os.Getenv("RACK_TOKEN")
	rackUsername := getEnv("RACK_USERNAME", "convox")

	rackHost = c.resolveRackHost(rackHost)

	if rackHost != "" && rackToken != "" {
		c.configureRack(rackHost, rackToken, rackUsername)
	} else if c.DevMode {
		c.setupDevRacks()
	}
}

func (_ *Config) resolveRackHost(rackHost string) string {
	if rackHost != "" {
		return rackHost
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return ""
	}
	rack := getEnv("RACK", "")
	if rack == "" {
		return ""
	}
	return fmt.Sprintf("https://api.%s-system.svc.cluster.local:5443", rack)
}

func (c *Config) configureRack(rackHost, rackToken, rackUsername string) {
	if !strings.HasPrefix(rackHost, "http://") && !strings.HasPrefix(rackHost, "https://") {
		rackHost = "http://" + rackHost
	}

	rackName := strings.TrimSpace(os.Getenv("RACK"))
	if rackName == "" {
		rackName = "default"
	}
	rackAlias := strings.TrimSpace(os.Getenv("RACK_ALIAS"))
	if rackAlias == "" {
		rackAlias = rackName
	}
	rackDisplayName := strings.TrimSpace(os.Getenv("RACK_DISPLAY_NAME"))
	if rackDisplayName == "" {
		rackDisplayName = rackAlias
	}

	c.Racks["default"] = RackConfig{
		Name:        rackName,
		Alias:       rackAlias,
		DisplayName: rackDisplayName,
		URL:         rackHost,
		Username:    rackUsername,
		APIKey:      rackToken,
		Enabled:     true,
	}
}

func (c *Config) setupDevRacks() {
	c.Racks["local"] = RackConfig{
		Name:     "local",
		Alias:    "local",
		URL:      "http://localhost:5443",
		Username: "convox",
		APIKey:   "dev-token",
		Region:   "us-east-1",
		Enabled:  true,
	}
}

func generateDevKey() (string, error) {
	b := make([]byte, 32)
	if _, err := randRead(b); err != nil {
		return "", fmt.Errorf("generateDevKey: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
