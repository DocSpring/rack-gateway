package cli

import (
	"net/http"
	"time"
)

var (
	// ConfigPath is the directory where config files are stored
	ConfigPath string

	// RackFlag is the --rack flag value for rack selection
	RackFlag string

	// APITokenFlag is the --api-token flag value
	APITokenFlag string

	// MFAMethodFlag is the --mfa-method flag value
	MFAMethodFlag string

	// MFACodeFlag is the --mfa-code flag value for pre-supplying TOTP codes
	MFACodeFlag string

	// Version is the CLI version (set by build)
	Version = "dev"

	// BuildTime is when the CLI was built (set by build)
	BuildTime = "unknown"

	// HTTPClient is the shared HTTP client
	HTTPClient = &http.Client{Timeout: 30 * time.Second}
)
