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
	Port                  string
	Domain                string
	JWTSecret             string
	JWTExpiry             time.Duration
	GoogleClientID        string
	GoogleClientSecret    string
	GoogleAllowedDomain   string
	GoogleOAuthBaseURL    string
	AdminUsers            []string
	ViewerUsers           []string
	DeployerUsers         []string
	OperationsUsers       []string
	DevMode               bool
	Racks                 map[string]RackConfig
	LogResponseBodies     bool
	LogResponseMaxBytes   int
	RackTLSPinningEnabled bool
	TrustedProxies        []string
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

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		Domain:              getEnv("DOMAIN", ""),
		JWTExpiry:           30 * 24 * time.Hour,
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleAllowedDomain: getEnv("GOOGLE_ALLOWED_DOMAIN", ""),
		GoogleOAuthBaseURL:  getEnv("GOOGLE_OAUTH_BASE_URL", ""),
		DevMode:             getEnv("DEV_MODE", "false") == "true",
		Racks:               make(map[string]RackConfig),
		LogResponseBodies:   getEnv("LOG_RESPONSE_BODIES", "false") == "true",
		LogResponseMaxBytes: 16384,
		// Disabled by default because the Convox rack API currently generates a fresh self-signed
		// certificate on every restart (see stdapi.Server.Listen). Pinning that dynamic cert would
		// break after each deploy. If Convox supports providing a stable internal certificate in the
		// future, operators can enable this flag to re-activate TOFU pinning.
		RackTLSPinningEnabled: getEnv("ENABLE_RACK_TLS_PINNING", "false") == "true",
	}
	if mb := getEnv("LOG_RESPONSE_MAX_BYTES", "65536"); mb != "" {
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
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
