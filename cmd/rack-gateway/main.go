package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DocSpring/rack-gateway/internal/cli"
)

func main() {
	// Set global config path
	defaultConfigPath := getEnv("GATEWAY_CLI_CONFIG_DIR", filepath.Join(homeDir(), ".config", "rack-gateway"))
	cli.ConfigPath = defaultConfigPath

	// Set version info
	cli.Version = version
	cli.BuildTime = buildTime

	// Execute CLI
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var (
	version   = "dev"
	buildTime = "unknown"
)

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return ""
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
