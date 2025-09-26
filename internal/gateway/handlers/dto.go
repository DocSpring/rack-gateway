package handlers

import (
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
)

// ErrorResponse represents a standard error payload.
type ErrorResponse struct {
	Error string `json:"error"`
}

// MFA enrollment and verification payloads.
type StartTOTPEnrollmentResponse struct {
	MethodID    int64    `json:"method_id"`
	Secret      string   `json:"secret"`
	URI         string   `json:"uri"`
	BackupCodes []string `json:"backup_codes"`
}

type ConfirmTOTPEnrollmentRequest struct {
	MethodID    int64  `json:"method_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	TrustDevice bool   `json:"trust_device"`
}

type VerifyMFARequest struct {
	Code        string `json:"code" binding:"required"`
	TrustDevice bool   `json:"trust_device"`
}

type VerifyMFAResponse struct {
	MFAVerifiedAt         time.Time `json:"mfa_verified_at"`
	RecentStepUpExpiresAt time.Time `json:"recent_step_up_expires_at"`
	TrustedDeviceCookie   bool      `json:"trusted_device_cookie"`
}

type BackupCodesResponse struct {
	BackupCodes []string `json:"backup_codes"`
}

type MFAMethodResponse struct {
	ID          int64      `json:"id"`
	Type        string     `json:"type"`
	Label       string     `json:"label,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

type TrustedDeviceResponse struct {
	ID            int64      `json:"id"`
	Label         string     `json:"label"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at"`
	IPAddress     string     `json:"ip_address,omitempty"`
	UserAgent     string     `json:"user_agent,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
}

type MFABackupCodesSummary struct {
	Total           int        `json:"total"`
	Unused          int        `json:"unused"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	LastGeneratedAt *time.Time `json:"last_generated_at,omitempty"`
}

type MFAStatusResponse struct {
	Enrolled              bool                    `json:"enrolled"`
	Required              bool                    `json:"required"`
	Methods               []MFAMethodResponse     `json:"methods"`
	TrustedDevices        []TrustedDeviceResponse `json:"trusted_devices"`
	BackupCodes           MFABackupCodesSummary   `json:"backup_codes"`
	RecentStepUpExpiresAt *time.Time              `json:"recent_step_up_expires_at,omitempty"`
}

// StatusResponse is returned for simple acknowledgment payloads.
type StatusResponse struct {
	Status string `json:"status"`
}

// HealthResponse represents the health check payload.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// RackSummary describes the default rack available to the current user.
type RackSummary struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Host  string `json:"host"`
}

// CurrentUserResponse describes the authenticated user's profile and permissions.
type CurrentUserResponse struct {
	Email                 string       `json:"email"`
	Name                  string       `json:"name"`
	Roles                 []string     `json:"roles"`
	Permissions           []string     `json:"permissions"`
	Rack                  *RackSummary `json:"rack,omitempty"`
	MFAEnrolled           bool         `json:"mfa_enrolled"`
	MFARequired           bool         `json:"mfa_required"`
	RecentStepUpExpiresAt *time.Time   `json:"recent_step_up_expires_at,omitempty"`
}

// EnvValuesResponse wraps environment variable key/value pairs.
type EnvValuesResponse struct {
	Env map[string]string `json:"env"`
}

// UpdateEnvValuesRequest defines the payload for updating environment variables.
type UpdateEnvValuesRequest struct {
	App    string            `json:"app" binding:"required"`
	Set    map[string]string `json:"set"`
	Remove []string          `json:"remove"`
}

// UpdateEnvValuesResponse is returned after successfully creating a new release with updated env vars.
type UpdateEnvValuesResponse struct {
	Env       map[string]string `json:"env"`
	ReleaseID string            `json:"release_id,omitempty"`
}

// CLILoginCompleteRequest represents the payload used to finish the CLI OAuth flow.
type CLILoginCompleteRequest struct {
	State         string `json:"state" binding:"required"`
	CodeVerifier  string `json:"code_verifier" binding:"required"`
	DeviceID      string `json:"device_id"`
	DeviceName    string `json:"device_name"`
	DeviceOS      string `json:"device_os"`
	ClientVersion string `json:"client_version"`
}

// CLILoginResponse represents the session token returned to the CLI.
type CLILoginResponse struct {
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

// UserSummary represents the minimal user payload returned by admin endpoints.
type UserSummary struct {
	Email          string   `json:"email"`
	Name           string   `json:"name"`
	Roles          []string `json:"roles"`
	CreatedByEmail string   `json:"created_by_email,omitempty"`
}

// CreateUserRequest defines the payload for creating a user.
type CreateUserRequest struct {
	Email string   `json:"email" binding:"required,email"`
	Name  string   `json:"name" binding:"required"`
	Roles []string `json:"roles" binding:"required,min=1"`
}

// UpdateUserProfileRequest defines the payload for updating user profile information.
type UpdateUserProfileRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UpdateUserRolesRequest defines the payload for updating user roles.
type UpdateUserRolesRequest struct {
	Roles []string `json:"roles" binding:"required,min=1"`
}

// CreateAPITokenRequest represents the request body for creating a new API token.
type CreateAPITokenRequest struct {
	Name        string   `json:"name" binding:"required"`
	UserEmail   string   `json:"user_email"`
	Permissions []string `json:"permissions"`
}

// CreateAPITokenResponse represents the response body for API token creation.
type CreateAPITokenResponse struct {
	Token       string      `json:"token"`
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	Permissions []string    `json:"permissions"`
	APIToken    db.APIToken `json:"api_token"`
}

// UpdateAPITokenRequest defines the payload for updating API token metadata.
type UpdateAPITokenRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// TokenPermissionMetadata includes the permission catalog for API tokens.
type TokenPermissionMetadata struct {
	Permissions        []string         `json:"permissions"`
	Roles              []RoleDescriptor `json:"roles"`
	UserRoles          []string         `json:"user_roles"`
	UserPermissions    []string         `json:"user_permissions"`
	DefaultPermissions []string         `json:"default_permissions"`
}

// RoleDescriptor describes an RBAC role exposed via the admin API.
type RoleDescriptor struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// AuditLogsResponse wraps paginated audit logs.
type AuditLogsResponse struct {
	Logs  []*db.AuditLog `json:"logs"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

// UserSessionResponse describes an individual active user session.
type UserSessionResponse struct {
	ID        int64       `json:"id"`
	CreatedAt string      `json:"created_at"`
	LastSeen  string      `json:"last_seen_at"`
	ExpiresAt string      `json:"expires_at"`
	IPAddress string      `json:"ip_address,omitempty"`
	UserAgent string      `json:"user_agent,omitempty"`
	Metadata  interface{} `json:"metadata,omitempty"`
}

// RevokeSessionResponse reports the outcome of a single-session revocation.
type RevokeSessionResponse struct {
	Revoked bool `json:"revoked"`
}

// RevokeAllSessionsResponse reports the number of sessions revoked in bulk.
type RevokeAllSessionsResponse struct {
	RevokedCount int64 `json:"revoked_count"`
}

// UpdateProtectedEnvVarsRequest defines the payload for updating protected environment variables.
type UpdateProtectedEnvVarsRequest struct {
	ProtectedEnvVars []string `json:"protected_env_vars"`
}

// UpdateAllowDestructiveActionsRequest defines the payload for toggling destructive actions.
type UpdateAllowDestructiveActionsRequest struct {
	AllowDestructiveActions bool `json:"allow_destructive_actions"`
}

// UpdateMFASettingsRequest defines the payload for updating MFA enforcement defaults.
type UpdateMFASettingsRequest struct {
	RequireAllUsers bool `json:"require_all_users"`
}
