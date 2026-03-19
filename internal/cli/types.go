package cli

import (
	"errors"
	"time"
)

// Config represents the CLI configuration file
type Config struct {
	Current   string                   `json:"current,omitempty"`
	Gateways  map[string]GatewayConfig `json:"gateways"`
	MachineID string                   `json:"machine_id,omitempty"`
	// Global default: "default", "webauthn", or "totp"
	MFAPreference string `json:"mfa_preference,omitempty"`
	// Global default: "default", "disabled", or "/path/to/file.mp3"
	NotificationSound string `json:"notification_sound,omitempty"`
	// Global default: sound volume (0.0 to 1.0, default 0.6 = 60%)
	SoundVolume *float64 `json:"sound_volume,omitempty"`
	// Racks to exclude when using --racks all (e.g., ["dev", "Dev"])
	AllRacksExclude []string `json:"all_racks_exclude,omitempty"`
}

// GatewayConfig represents configuration for a single gateway/rack
type GatewayConfig struct {
	URL         string    `json:"url"`
	Token       string    `json:"token,omitempty"`
	Email       string    `json:"email,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	SessionID   int64     `json:"session_id,omitempty"`
	Channel     string    `json:"channel,omitempty"`
	DeviceID    string    `json:"device_id,omitempty"`
	DeviceName  string    `json:"device_name,omitempty"`
	MFAVerified bool      `json:"mfa_verified,omitempty"`
	// Per-rack override: "default", "webauthn", or "totp"
	MFAPreference string `json:"mfa_preference,omitempty"`
	// Per-rack override: "default", "disabled", or "/path/to/file.mp3"
	NotificationSound string `json:"notification_sound,omitempty"`
	// Per-rack override: sound volume (0.0 to 1.0, default 0.6 = 60%)
	SoundVolume *float64 `json:"sound_volume,omitempty"`
}

// RackStatus contains information about the current rack
type RackStatus struct {
	Rack        string
	GatewayURL  string
	StatusLines []string
}

// LoginStartResponse is the response from /api/v1/auth/cli/start
type LoginStartResponse struct {
	AuthURL      string `json:"auth_url"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

// LoginCallbackRequest is the request to /api/v1/auth/cli/complete
type LoginCallbackRequest struct {
	Code         string `json:"code"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

// LoginResponse is the response from /api/v1/auth/cli/complete
type LoginResponse struct {
	Token              string    `json:"token"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	ExpiresAt          time.Time `json:"expires_at"`
	SessionID          int64     `json:"session_id"`
	Channel            string    `json:"channel"`
	DeviceID           string    `json:"device_id"`
	DeviceName         string    `json:"device_name"`
	MFAVerified        bool      `json:"mfa_verified"`
	MFARequired        bool      `json:"mfa_required"`
	EnrollmentRequired bool      `json:"enrollment_required"`
}

// DeviceInfo contains information about the CLI client device
type DeviceInfo struct {
	ID            string
	Name          string
	OS            string
	ClientVersion string
}

// MFAStatusResponse is the response from /api/v1/auth/mfa/status
type MFAStatusResponse struct {
	Enrolled              bool                `json:"enrolled"`
	Required              bool                `json:"required"`
	Methods               []MFAMethodResponse `json:"methods"`
	PreferredMethod       *string             `json:"preferred_method,omitempty"`
	RecentStepUpExpiresAt *time.Time          `json:"recent_step_up_expires_at,omitempty"`
}

// MFAMethodResponse represents a single MFA method
type MFAMethodResponse struct {
	ID          int64  `json:"id"`
	Type        string `json:"type"`
	Label       string `json:"label"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
	IsEnrolling bool   `json:"is_enrolling"`
}

// ErrLoginPending is returned when login is still pending browser completion
var ErrLoginPending = errors.New("login pending")

// GatewayInfoResponse is the response from /api/v1/info
type GatewayInfoResponse struct {
	User         GatewayUserInfo         `json:"user"`
	Rack         GatewayRackSummary      `json:"rack"`
	Integrations GatewayIntegrationsInfo `json:"integrations"`
	Version      GatewayVersionInfo      `json:"version"`
}

// GatewayUserInfo contains user information from the gateway
type GatewayUserInfo struct {
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// GatewayRackSummary contains rack summary from the gateway
type GatewayRackSummary struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Host  string `json:"host"`
}

// GatewayIntegrationsInfo contains integration status
type GatewayIntegrationsInfo struct {
	Slack    bool `json:"slack"`
	GitHub   bool `json:"github"`
	CircleCI bool `json:"circleci"`
}

// GatewayVersionInfo contains gateway version information
type GatewayVersionInfo struct {
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
}
