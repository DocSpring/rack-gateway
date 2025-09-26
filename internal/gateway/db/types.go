package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Database wraps the SQL database connection
type Database struct {
	db     *sql.DB
	driver string // always "pgx"
}

// User represents a user in the system
type User struct {
	ID              int64      `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	Roles           []string   `json:"roles"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Suspended       bool       `json:"suspended"`
	MFAEnrolled     bool       `json:"mfa_enrolled"`
	MFAEnforcedAt   *time.Time `json:"mfa_enforced_at,omitempty"`
	CreatedByUserID *int64     `json:"created_by_user_id,omitempty"`
	CreatedByEmail  string     `json:"created_by_email,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
}

// APIToken represents an API token for CI/CD
type APIToken struct {
	ID              int64      `json:"id"`
	TokenHash       string     `json:"-"` // Never expose the actual token
	Name            string     `json:"name"`
	UserID          int64      `json:"user_id"`
	CreatedByUserID *int64     `json:"created_by_user_id,omitempty"`
	CreatedByEmail  string     `json:"created_by_email,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
	Permissions     []string   `json:"permissions"`
	CreatedAt       time.Time  `json:"created_at" ts_type:"string"`
	ExpiresAt       *time.Time `json:"expires_at" ts_type:"string | null"`
	LastUsedAt      *time.Time `json:"last_used_at" ts_type:"string | null"`
}

// UserSession represents an authenticated web session stored in the database.
type UserSession struct {
	ID              int64           `json:"id"`
	UserID          int64           `json:"user_id"`
	TokenHash       string          `json:"-"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	LastSeenAt      time.Time       `json:"last_seen_at"`
	ExpiresAt       time.Time       `json:"expires_at"`
	Channel         string          `json:"channel"`
	DeviceID        string          `json:"device_id,omitempty"`
	DeviceName      string          `json:"device_name,omitempty"`
	MFAVerifiedAt   *time.Time      `json:"mfa_verified_at,omitempty"`
	RecentStepUpAt  *time.Time      `json:"recent_step_up_at,omitempty"`
	TrustedDeviceID *int64          `json:"trusted_device_id,omitempty"`
	RevokedAt       *time.Time      `json:"revoked_at,omitempty"`
	RevokedByUser   *int64          `json:"revoked_by_user_id,omitempty"`
	IPAddress       string          `json:"ip_address,omitempty"`
	UserAgent       string          `json:"user_agent,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	DeviceMetadata  json.RawMessage `json:"device_metadata,omitempty"`
}

// RackTLSCert stores the pinned rack TLS certificate information.
type RackTLSCert struct {
	PEM         string    `json:"pem"`
	Fingerprint string    `json:"fingerprint"`
	FetchedAt   time.Time `json:"fetched_at" ts_type:"string"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	UserEmail      string    `json:"user_email"`
	UserName       string    `json:"user_name,omitempty"`
	ActionType     string    `json:"action_type"` // "convox", "users", "auth"
	Action         string    `json:"action"`      // e.g., "env.read", "user.create", "auth.failed"
	Command        string    `json:"command,omitempty"`
	Resource       string    `json:"resource,omitempty"`
	ResourceType   string    `json:"resource_type,omitempty"`
	Details        string    `json:"details,omitempty"` // JSON string
	IPAddress      string    `json:"ip_address,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	Status         string    `json:"status"`                  // "success", "denied", "error", "blocked"
	RBACDecision   string    `json:"rbac_decision,omitempty"` // "allow" or "deny"
	HTTPStatus     int       `json:"http_status,omitempty"`
	ResponseTimeMs int       `json:"response_time_ms"`
	EventCount     int       `json:"event_count"`
}

// UserResource represents a creator->resource mapping
type UserResource struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreatorInfo struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// MFAMethod represents a configured MFA factor for a user.
type MFAMethod struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	Type         string     `json:"type"`
	Label        string     `json:"label,omitempty"`
	Secret       string     `json:"-"`
	CredentialID []byte     `json:"credential_id,omitempty"`
	PublicKey    []byte     `json:"public_key,omitempty"`
	Transports   []string   `json:"transports,omitempty"`
	Metadata     []byte     `json:"metadata,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ConfirmedAt  *time.Time `json:"confirmed_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

// MFABackupCode represents a single-use backup code for MFA.
type MFABackupCode struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	CodeHash  string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// TrustedDevice tracks a trusted device token for MFA bypass.
type TrustedDevice struct {
	ID            int64           `json:"id"`
	UserID        int64           `json:"user_id"`
	DeviceID      string          `json:"device_id"`
	TokenHash     string          `json:"-"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ExpiresAt     time.Time       `json:"expires_at"`
	LastUsedAt    time.Time       `json:"last_used_at"`
	IPFirst       string          `json:"ip_first,omitempty"`
	IPLast        string          `json:"ip_last,omitempty"`
	UserAgentHash string          `json:"user_agent_hash,omitempty"`
	RevokedAt     *time.Time      `json:"revoked_at,omitempty"`
	RevokedReason string          `json:"revoked_reason,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}
