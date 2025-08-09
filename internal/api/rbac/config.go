package rbac

import (
	"fmt"
	"os"
	
	"gopkg.in/yaml.v3"
)

// GatewayConfig represents the full config.yml structure
type GatewayConfig struct {
	Domain string                  `yaml:"domain"`
	Users  map[string]*UserConfig  `yaml:"users"`
}

// UserConfig represents a user in config.yml
type UserConfig struct {
	Name  string   `yaml:"name"`
	Roles []string `yaml:"roles"`
}

// LoadConfig loads the gateway configuration from config.yml
func LoadConfig(configPath string) (*GatewayConfig, error) {
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return empty config if file doesn't exist
		return &GatewayConfig{
			Domain: "",
			Users:  make(map[string]*UserConfig),
		}, nil
	}
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config GatewayConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	// Initialize users map if nil
	if config.Users == nil {
		config.Users = make(map[string]*UserConfig)
	}
	
	return &config, nil
}