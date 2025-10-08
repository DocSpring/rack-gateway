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
	Port                    string
	Domain                  string
	SentryDSN               string
	SentryEnvironment       string
	SentryRelease           string
	SentryJSDsn             string
	SentryJSTracesRate      string
	SentryTestsEnabled      bool
	JWTSecret               string
	SessionIdleTimeout      time.Duration
	GoogleClientID          string
	GoogleClientSecret      string
	GoogleAllowedDomain     string
	GoogleOAuthBaseURL      string
	SlackClientID           string
	SlackClientSecret       string
	AdminUsers              []string
	ViewerUsers             []string
	DeployerUsers           []string
	OperationsUsers         []string
	DevMode                 bool
	Racks                   map[string]RackConfig
	LogResponseBodies       bool
	LogResponseMaxBytes     int
	RackTLSPinningEnabled   bool
	TrustedProxies          []string
	DeployApprovalsDisabled bool
	DeployApprovalWindow    time.Duration
}

type RackConfig struct {
	Name     string
	Alias    string
	URL      string
	Username string // Username for Basic Auth (default: "convox")
	APIKey   string // Password for Basic Auth
	Region   string
	Enabled  bool
}

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

func Load() (*Config, error) {
	release := strings.TrimSpace(getEnv("SENTRY_RELEASE", ""))
	if release == "" {
		release = strings.TrimSpace(os.Getenv("RELEASE"))
	}

	jsTracesRate := normalizeSampleRate(getEnv("SENTRY_JS_TRACES_SAMPLE_RATE", ""), "0")

	cfg := &Config{
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
		LogResponseBodies:   getEnv("LOG_RESPONSE_BODIES", "false") == "true",
		LogResponseMaxBytes: 16384,
		// Disabled by default because the Convox rack API currently generates a fresh self-signed
		// certificate on every restart (see stdapi.Server.Listen). Pinning that dynamic cert would
		// break after each deploy. If Convox supports providing a stable internal certificate in the
		// future, operators can enable this flag to re-activate TOFU pinning.
		RackTLSPinningEnabled:   getEnv("ENABLE_RACK_TLS_PINNING", "false") == "true",
		DeployApprovalsDisabled: getEnv("DISABLE_DEPLOY_APPROVALS", "false") == "true",
		DeployApprovalWindow:    15 * time.Minute,
	}
	if mb := getEnv("LOG_RESPONSE_MAX_BYTES", "65536"); mb != "" {
		if v, err := strconv.Atoi(mb); err == nil && v > 0 {
			cfg.LogResponseMaxBytes = v
		}
	}

	jwtKey := getEnv("APP_SECRET_KEY", "")
	if jwtKey == "" {
		if cfg.DevMode {
			jwtKey = generateDevKey()
			fmt.Printf("Generated dev secret key: %s\n", jwtKey)
		} else {
			return nil, fmt.Errorf("APP_SECRET_KEY is required in production")
		}
	}
	cfg.JWTSecret = jwtKey

	// Session idle timeout defaults to 5 minutes to enforce rapid re-auth on inactivity.
	cfg.SessionIdleTimeout = 5 * time.Minute
	if raw := strings.TrimSpace(getEnv("SESSION_IDLE_TIMEOUT", "")); raw != "" {
		if dur, err := time.ParseDuration(raw); err == nil && dur > 0 {
			cfg.SessionIdleTimeout = dur
		}
	}

	adminUsers := getEnv("ADMIN_USERS", "")
	if adminUsers != "" {
		cfg.AdminUsers = strings.Split(adminUsers, ",")
	}
	viewerUsers := getEnv("VIEWER_USERS", "")
	if viewerUsers != "" {
		cfg.ViewerUsers = strings.Split(viewerUsers, ",")
	}
	deployerUsers := getEnv("DEPLOYER_USERS", "")
	if deployerUsers != "" {
		cfg.DeployerUsers = strings.Split(deployerUsers, ",")
	}
	operationsUsers := getEnv("OPERATIONS_USERS", "")
	if operationsUsers != "" {
		cfg.OperationsUsers = strings.Split(operationsUsers, ",")
	}

	if proxies := strings.TrimSpace(getEnv("TRUSTED_PROXY_CIDRS", "")); proxies != "" {
		for _, entry := range strings.Split(proxies, ",") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				cfg.TrustedProxies = append(cfg.TrustedProxies, entry)
			}
		}
	}

	cfg.loadRacksFromEnv()

	if window := strings.TrimSpace(getEnv("DEPLOY_APPROVAL_WINDOW", "")); window != "" {
		if dur, err := time.ParseDuration(window); err == nil && dur > 0 {
			cfg.DeployApprovalWindow = dur
		}
	}

	return cfg, nil
}

func (c *Config) loadRacksFromEnv() {
	// Load the single rack this gateway is protecting
	rackHost := os.Getenv("RACK_HOST")
	rackToken := os.Getenv("RACK_TOKEN")
	rackUsername := getEnv("RACK_USERNAME", "convox") // Default to "convox" for standard Convox racks

	// Auto-infer RACK_HOST when running in Kubernetes and RACK is set (Convox convention)
	if rackHost == "" {
		if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
			if rack := getEnv("RACK", ""); rack != "" {
				// Convox API service DNS format: api.<rack>-system.svc.cluster.local:5443 (TLS on 5443)
				rackHost = fmt.Sprintf("https://api.%s-system.svc.cluster.local:5443", rack)
			}
		}
	}

	if rackHost != "" && rackToken != "" {
		// Default to http:// if no protocol specified. Convox router endpoints are plain HTTP by default.
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

		// The gateway protects a single rack
		c.Racks["default"] = RackConfig{
			Name:     rackName,
			Alias:    rackAlias,
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
		Alias:    "local",
		URL:      "http://localhost:5443",
		Username: "convox",
		APIKey:   "dev-token",
		Region:   "us-east-1",
		Enabled:  true,
	}
}

func generateDevKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate dev key: %v", err))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
