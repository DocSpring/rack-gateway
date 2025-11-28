package version

// Variables are set at build time via ldflags
var (
	// Version is the application version (e.g., "0.0.9")
	Version = "dev"

	// CommitHash is the git commit hash
	CommitHash = "unknown"
)
