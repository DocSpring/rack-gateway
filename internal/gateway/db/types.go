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
	logSQL bool   // log all SQL queries
}

// User represents a user in the system
type User struct {
	ID                 int64      `json:"id"`
	Email              string     `json:"email"`
	Name               string     `json:"name"`
	Roles              []string   `json:"roles"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Suspended          bool       `json:"suspended"`
	MFAEnrolled        bool       `json:"mfa_enrolled"`
	MFAEnforcedAt      *time.Time `json:"mfa_enforced_at,omitempty"`
	PreferredMFAMethod *string    `json:"preferred_mfa_method,omitempty"`
	LockedAt           *time.Time `json:"locked_at,omitempty"`
	LockedReason       string     `json:"locked_reason,omitempty"`
	LockedByUserID     *int64     `json:"locked_by_user_id,omitempty"`
	LockedByEmail      string     `json:"locked_by_email,omitempty"`
	LockedByName       string     `json:"locked_by_name,omitempty"`
	CreatedByUserID    *int64     `json:"created_by_user_id,omitempty"`
	CreatedByEmail     string     `json:"created_by_email,omitempty"`
	CreatedByName      string     `json:"created_by_name,omitempty"`
}

// APIToken represents an API token for CI/CD
type APIToken struct {
	ID              int64      `json:"id"`
	PublicID        string     `json:"public_id"`
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
	ID                      int64     `json:"id"`
	Timestamp               time.Time `json:"timestamp"`
	UserEmail               string    `json:"user_email"`
	UserName                string    `json:"user_name,omitempty"`
	APITokenID              *int64    `json:"api_token_id,omitempty"`
	APITokenName            string    `json:"api_token_name,omitempty"`
	ActionType              string    `json:"action_type"` // "convox", "users", "auth"
	Action                  string    `json:"action"`      // e.g., "env.read", "user.create", "login.oauth_failed"
	Command                 string    `json:"command,omitempty"`
	Resource                string    `json:"resource,omitempty"`
	ResourceType            string    `json:"resource_type,omitempty"`
	Details                 string    `json:"details,omitempty"` // JSON string
	IPAddress               string    `json:"ip_address,omitempty"`
	UserAgent               string    `json:"user_agent,omitempty"`
	Status                  string    `json:"status"`                  // "success", "denied", "error", "blocked"
	RBACDecision            string    `json:"rbac_decision,omitempty"` // "allow" or "deny"
	HTTPStatus              int       `json:"http_status,omitempty"`
	ResponseTimeMs          int       `json:"response_time_ms"`
	EventCount              int       `json:"event_count"`
	DeployApprovalRequestID *int64    `json:"deploy_approval_request_id,omitempty"`
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

// DeployApprovalRequest tracks manual approval requirements for CI/CD actions.
type DeployApprovalRequest struct {
	ID                          int64           `json:"id"`
	PublicID                    string          `json:"public_id"`
	Message                     string          `json:"message"`
	Status                      string          `json:"status"`
	CreatedAt                   time.Time       `json:"created_at" ts_type:"string"`
	UpdatedAt                   time.Time       `json:"updated_at" ts_type:"string"`
	CreatedByUserID             *int64          `json:"created_by_user_id,omitempty"`
	CreatedByEmail              string          `json:"created_by_email,omitempty"`
	CreatedByName               string          `json:"created_by_name,omitempty"`
	CreatedByAPITokenID         *int64          `json:"created_by_api_token_id,omitempty"`
	CreatedByAPITokenPublicID   string          `json:"created_by_api_token_public_id,omitempty"`
	CreatedByAPITokenName       string          `json:"created_by_api_token_name,omitempty"`
	TargetAPITokenID            int64           `json:"target_api_token_id"`
	TargetAPITokenPublicID      string          `json:"target_api_token_public_id"`
	TargetAPITokenName          string          `json:"target_api_token_name,omitempty"`
	TargetUserID                *int64          `json:"target_user_id,omitempty"`
	ApprovedByUserID            *int64          `json:"approved_by_user_id,omitempty"`
	ApprovedByEmail             string          `json:"approved_by_email,omitempty"`
	ApprovedByName              string          `json:"approved_by_name,omitempty"`
	ApprovedAt                  *time.Time      `json:"approved_at,omitempty" ts_type:"string | null"`
	ApprovalExpiresAt           *time.Time      `json:"approval_expires_at,omitempty" ts_type:"string | null"`
	RejectedByUserID            *int64          `json:"rejected_by_user_id,omitempty"`
	RejectedByEmail             string          `json:"rejected_by_email,omitempty"`
	RejectedByName              string          `json:"rejected_by_name,omitempty"`
	RejectedAt                  *time.Time      `json:"rejected_at,omitempty" ts_type:"string | null"`
	ApprovalNotes               string          `json:"approval_notes,omitempty"`
	GitCommitHash               string          `json:"git_commit_hash"`
	GitBranch                   string          `json:"git_branch,omitempty"`
	PipelineURL                 string          `json:"pipeline_url,omitempty"`
	PrURL                       string          `json:"pr_url,omitempty"`
	CIMetadata                  json.RawMessage `json:"ci_metadata,omitempty"`
	App                         string          `json:"app,omitempty"`
	ObjectURL                   string          `json:"object_url,omitempty"`
	BuildID                     string          `json:"build_id,omitempty"`
	ReleaseID                   string          `json:"release_id,omitempty"`
	ProcessIDs                  []string        `json:"process_ids,omitempty"`
	ExecCommands                json.RawMessage `json:"exec_commands,omitempty"`
	ReleaseCreatedAt            *time.Time      `json:"release_created_at,omitempty" ts_type:"string | null"`
	ReleasePromotedAt           *time.Time      `json:"release_promoted_at,omitempty" ts_type:"string | null"`
	ReleasePromotedByAPITokenID *int64          `json:"release_promoted_by_api_token_id,omitempty"`
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

// MFATOTPAttempt tracks all TOTP verification attempts for replay protection, rate limiting, and audit.
type MFATOTPAttempt struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	MethodID      *int64    `json:"method_id,omitempty"`
	CodeHash      string    `json:"-"`
	Success       bool      `json:"success"`
	AttemptedAt   time.Time `json:"attempted_at"`
	IPAddress     string    `json:"ip_address,omitempty"`
	UserAgent     string    `json:"user_agent,omitempty"`
	FailureReason string    `json:"failure_reason,omitempty"`
	SessionID     *int64    `json:"session_id,omitempty"`
}

// MFAWebAuthnAttempt tracks all WebAuthn verification attempts for rate limiting and audit.
type MFAWebAuthnAttempt struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	MethodID      *int64    `json:"method_id,omitempty"`
	Success       bool      `json:"success"`
	AttemptedAt   time.Time `json:"attempted_at"`
	IPAddress     string    `json:"ip_address,omitempty"`
	UserAgent     string    `json:"user_agent,omitempty"`
	FailureReason string    `json:"failure_reason,omitempty"`
	SessionID     *int64    `json:"session_id,omitempty"`
}
