package handlers

import (
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ErrorResponse represents a standard error payload.
type ErrorResponse struct {
	Error string `json:"error" validate:"required"`
}

// MFA enrollment and verification payloads.
type StartTOTPEnrollmentResponse struct {
	MethodID    int64    `json:"method_id" validate:"required"`
	Secret      string   `json:"secret" validate:"required"`
	URI         string   `json:"uri" validate:"required"`
	BackupCodes []string `json:"backup_codes" validate:"required"`
}

type ConfirmTOTPEnrollmentRequest struct {
	MethodID    int64  `json:"method_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	TrustDevice bool   `json:"trust_device"`
	Label       string `json:"label"`
}

type StartYubiOTPEnrollmentRequest struct {
	YubiOTP string `json:"yubi_otp" binding:"required"`
	Label   string `json:"label"`
}

type StartYubiOTPEnrollmentResponse struct {
	MethodID    int64    `json:"method_id" validate:"required"`
	BackupCodes []string `json:"backup_codes,omitempty"`
}

type StartWebAuthnEnrollmentResponse struct {
	MethodID         int64       `json:"method_id" validate:"required"`
	PublicKeyOptions interface{} `json:"public_key_options" validate:"required"`
	BackupCodes      []string    `json:"backup_codes,omitempty"`
}

type ConfirmWebAuthnEnrollmentRequest struct {
	MethodID   int64       `json:"method_id" binding:"required"`
	Credential interface{} `json:"credential" binding:"required"`
	Label      string      `json:"label"`
}

type VerifyMFARequest struct {
	Code        string `json:"code" binding:"required"`
	TrustDevice bool   `json:"trust_device"`
}

type VerifyMFAResponse struct {
	MFAVerifiedAt         time.Time `json:"mfa_verified_at" validate:"required"`
	RecentStepUpExpiresAt time.Time `json:"recent_step_up_expires_at" validate:"required"`
	TrustedDeviceCookie   bool      `json:"trusted_device_cookie" validate:"required"`
}

type WebAuthnAssertionStartResponse struct {
	Options     interface{} `json:"options" validate:"required"`      // protocol.CredentialAssertion
	SessionData string      `json:"session_data" validate:"required"` // Serialized session to send back with verification
}

type VerifyWebAuthnAssertionRequest struct {
	SessionData       string `json:"session_data" binding:"required"`
	AssertionResponse string `json:"assertion_response" binding:"required"`
	TrustDevice       bool   `json:"trust_device"`
}

type UpdateMFAMethodRequest struct {
	Label string `json:"label" binding:"required,max=150"`
}

type BackupCodesResponse struct {
	BackupCodes []string `json:"backup_codes" validate:"required"`
}

type MFAMethodResponse struct {
	ID          int64      `json:"id" validate:"required"`
	Type        string     `json:"type" validate:"required"`
	Label       string     `json:"label,omitempty"`
	CreatedAt   time.Time  `json:"created_at" validate:"required"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

type TrustedDeviceResponse struct {
	ID            int64      `json:"id" validate:"required"`
	Label         string     `json:"label" validate:"required"`
	CreatedAt     time.Time  `json:"created_at" validate:"required"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at" validate:"required"`
	IPAddress     string     `json:"ip_address,omitempty"`
	UserAgent     string     `json:"user_agent,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
}

type MFABackupCodesSummary struct {
	Total           int        `json:"total" validate:"required"`
	Unused          int        `json:"unused" validate:"required"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	LastGeneratedAt *time.Time `json:"last_generated_at,omitempty"`
}

type MFAStatusResponse struct {
	Enrolled              bool                    `json:"enrolled" validate:"required"`
	Required              bool                    `json:"required" validate:"required"`
	Methods               []MFAMethodResponse     `json:"methods" validate:"required"`
	TrustedDevices        []TrustedDeviceResponse `json:"trusted_devices" validate:"required"`
	BackupCodes           MFABackupCodesSummary   `json:"backup_codes" validate:"required"`
	RecentStepUpExpiresAt *time.Time              `json:"recent_step_up_expires_at,omitempty"`
	PreferredMethod       *string                 `json:"preferred_method,omitempty"`
	WebAuthnAvailable     bool                    `json:"webauthn_available" validate:"required"`
}

// StatusResponse is returned for simple acknowledgment payloads.
type StatusResponse struct {
	Status string `json:"status" validate:"required"`
}

// WebAuthnEnrollmentResponse is returned after successful WebAuthn enrollment
type WebAuthnEnrollmentResponse struct {
	Status   string `json:"status" validate:"required"`
	MethodID int64  `json:"method_id" validate:"required"`
}

// HealthResponse represents the health check payload.
type HealthResponse struct {
	Status  string `json:"status" validate:"required"`
	Service string `json:"service" validate:"required"`
}

// RackSummary describes the default rack available to the current user.
type RackSummary struct {
	Name  string `json:"name" validate:"required"`
	Alias string `json:"alias" validate:"required"`
	Host  string `json:"host" validate:"required"`
}

// UserInfo describes the user portion of the info endpoint
type UserInfo struct {
	Email                 string     `json:"email" validate:"required"`
	Name                  string     `json:"name" validate:"required"`
	Roles                 []string   `json:"roles" validate:"required"`
	MFAEnrolled           bool       `json:"mfa_enrolled" validate:"required"`
	MFARequired           bool       `json:"mfa_required" validate:"required"`
	PreferredMFAMethod    *string    `json:"preferred_mfa_method,omitempty"`
	RecentStepUpExpiresAt *time.Time `json:"recent_step_up_expires_at,omitempty"`
	HasTrustedDevice      bool       `json:"has_trusted_device" validate:"required"`
}

// IntegrationsInfo describes which external integrations are configured
type IntegrationsInfo struct {
	Slack    bool `json:"slack" validate:"required"`
	GitHub   bool `json:"github" validate:"required"`
	CircleCI bool `json:"circleci" validate:"required"`
}

// InfoResponse provides bootstrap information for the frontend
type InfoResponse struct {
	User         UserInfo         `json:"user" validate:"required"`
	Rack         RackSummary      `json:"rack" validate:"required"`
	Integrations IntegrationsInfo `json:"integrations" validate:"required"`
}

// EnvValuesResponse wraps environment variable key/value pairs.
type EnvValuesResponse struct {
	Env map[string]string `json:"env" validate:"required"`
}

// UpdateEnvValuesRequest defines the payload for updating environment variables.
type UpdateEnvValuesRequest struct {
	App    string            `json:"app" binding:"required"`
	Set    map[string]string `json:"set"`
	Remove []string          `json:"remove"`
}

// UpdateEnvValuesResponse is returned after successfully creating a new release with updated env vars.
type UpdateEnvValuesResponse struct {
	Env       map[string]string `json:"env" validate:"required"`
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
	Email          string   `json:"email" validate:"required"`
	Name           string   `json:"name" validate:"required"`
	Roles          []string `json:"roles" validate:"required"`
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
	Role        string   `json:"role"`        // Role shortcut (viewer, ops, deployer, cicd, admin)
	Permissions []string `json:"permissions"` // Explicit permissions (overrides role)
}

// CreateAPITokenResponse represents the response body for API token creation.
type CreateAPITokenResponse struct {
	Token       string      `json:"token" validate:"required"`
	ID          int64       `json:"id" validate:"required"`
	Name        string      `json:"name" validate:"required"`
	Permissions []string    `json:"permissions" validate:"required"`
	APIToken    db.APIToken `json:"api_token" validate:"required"`
}

// UpdateAPITokenRequest defines the payload for updating API token metadata.
type UpdateAPITokenRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// TokenPermissionMetadata includes the permission catalog for API tokens.
type TokenPermissionMetadata struct {
	Permissions        []string         `json:"permissions" validate:"required"`
	Roles              []RoleDescriptor `json:"roles" validate:"required"`
	UserRoles          []string         `json:"user_roles" validate:"required"`
	UserPermissions    []string         `json:"user_permissions" validate:"required"`
	DefaultPermissions []string         `json:"default_permissions" validate:"required"`
}

// RoleDescriptor describes an RBAC role exposed via the admin API.
type RoleDescriptor struct {
	Name        string   `json:"name" validate:"required"`
	Label       string   `json:"label" validate:"required"`
	Description string   `json:"description" validate:"required"`
	Permissions []string `json:"permissions" validate:"required"`
}

// AuditLogsResponse wraps paginated audit logs.
type AuditLogsResponse struct {
	Logs  []*db.AuditLog `json:"logs" validate:"required"`
	Total int            `json:"total" validate:"required"`
	Page  int            `json:"page" validate:"required"`
	Limit int            `json:"limit" validate:"required"`
}

// UserSessionResponse describes an individual active user session.
type UserSessionResponse struct {
	ID        int64       `json:"id" validate:"required"`
	CreatedAt string      `json:"created_at" validate:"required"`
	LastSeen  string      `json:"last_seen_at" validate:"required"`
	ExpiresAt string      `json:"expires_at" validate:"required"`
	Channel   string      `json:"channel" validate:"required"`
	IPAddress string      `json:"ip_address,omitempty"`
	UserAgent string      `json:"user_agent,omitempty"`
	Metadata  interface{} `json:"metadata,omitempty"`
}

// RevokeSessionResponse reports the outcome of a single-session revocation.
type RevokeSessionResponse struct {
	Revoked bool `json:"revoked" validate:"required"`
}

// RevokeAllSessionsResponse reports the number of sessions revoked in bulk.
type RevokeAllSessionsResponse struct {
	RevokedCount int64 `json:"revoked_count" validate:"required"`
}

// UpdateProtectedEnvVarsRequest defines the payload for updating protected environment variables.
type UpdateProtectedEnvVarsRequest struct {
	ProtectedEnvVars []string `json:"protected_env_vars"`
}

// UpdateApprovedCommandsRequest defines the payload for updating approved commands for CI/CD exec.
type UpdateApprovedCommandsRequest struct {
	ApprovedCommands []string `json:"approved_commands"`
}

// UpdateAppImagePatternsRequest defines the payload for updating app image tag patterns.
type UpdateAppImagePatternsRequest struct {
	AppImagePatterns map[string]string `json:"app_image_patterns"`
}

// UpdateAllowDestructiveActionsRequest defines the payload for toggling destructive actions.
type UpdateAllowDestructiveActionsRequest struct {
	AllowDestructiveActions bool `json:"allow_destructive_actions"`
}

// UpdateMFASettingsRequest defines the payload for updating MFA enforcement defaults.
type UpdateMFASettingsRequest struct {
	RequireAllUsers bool `json:"require_all_users"`
}

// UpdatePreferredMFAMethodRequest defines the payload for updating user's preferred MFA method.
type UpdatePreferredMFAMethodRequest struct {
	PreferredMethod *string `json:"preferred_method"` // "totp", "webauthn", or null to clear
}

// CreateDeployApprovalRequestRequest represents the payload to open a deploy approval request.
type CreateDeployApprovalRequestRequest struct {
	Message            string                 `json:"message" binding:"required"`
	App                string                 `json:"app" binding:"required"`
	GitCommitHash      string                 `json:"git_commit_hash" binding:"required"`
	GitBranch          string                 `json:"git_branch,omitempty"`
	PipelineURL        string                 `json:"pipeline_url,omitempty"`
	CIProvider         string                 `json:"ci_provider,omitempty"`
	CIMetadata         map[string]interface{} `json:"ci_metadata,omitempty"`
	TargetAPITokenID   *string                `json:"target_api_token_id,omitempty"`
	TargetAPITokenName string                 `json:"target_api_token,omitempty"`
}

// UpdateDeployApprovalRequestStatusRequest carries optional admin notes when approving/rejecting.
type UpdateDeployApprovalRequestStatusRequest struct {
	Notes string `json:"notes"`
}

// DeployApprovalRequestResponse exposes deploy approval state to the CLI and admin UI.
type DeployApprovalRequestResponse struct {
	PublicID                  string                 `json:"public_id" validate:"required"`
	Message                   string                 `json:"message" validate:"required"`
	Status                    string                 `json:"status" validate:"required"`
	CreatedAt                 time.Time              `json:"created_at" ts_type:"string" validate:"required"`
	UpdatedAt                 time.Time              `json:"updated_at" ts_type:"string" validate:"required"`
	CreatedByEmail            string                 `json:"created_by_email,omitempty"`
	CreatedByName             string                 `json:"created_by_name,omitempty"`
	CreatedByAPITokenPublicID string                 `json:"created_by_api_token_id,omitempty"`
	CreatedByAPITokenName     string                 `json:"created_by_api_token_name,omitempty"`
	TargetAPITokenID          string                 `json:"target_api_token_id" validate:"required"`
	TargetAPITokenName        string                 `json:"target_api_token_name,omitempty"`
	ApprovedByEmail           string                 `json:"approved_by_email,omitempty"`
	ApprovedByName            string                 `json:"approved_by_name,omitempty"`
	ApprovedAt                *time.Time             `json:"approved_at,omitempty" ts_type:"string"`
	ApprovalExpiresAt         *time.Time             `json:"approval_expires_at,omitempty" ts_type:"string"`
	RejectedByEmail           string                 `json:"rejected_by_email,omitempty"`
	RejectedByName            string                 `json:"rejected_by_name,omitempty"`
	RejectedAt                *time.Time             `json:"rejected_at,omitempty" ts_type:"string"`
	ApprovalNotes             string                 `json:"approval_notes,omitempty"`
	GitCommitHash             string                 `json:"git_commit_hash" validate:"required"`
	GitBranch                 string                 `json:"git_branch,omitempty"`
	PipelineURL               string                 `json:"pipeline_url,omitempty"`
	PrURL                     string                 `json:"pr_url,omitempty"`
	CIProvider                string                 `json:"ci_provider,omitempty"`
	CIMetadata                map[string]interface{} `json:"ci_metadata,omitempty"`
	App                       string                 `json:"app,omitempty"`
	ObjectURL                 string                 `json:"object_url,omitempty"`
	BuildID                   string                 `json:"build_id,omitempty"`
	ReleaseID                 string                 `json:"release_id,omitempty"`
	ProcessIDs                []string               `json:"process_ids,omitempty"`
	ExecCommands              map[string]interface{} `json:"exec_commands,omitempty"`
	ReleaseCreatedAt          *time.Time             `json:"release_created_at,omitempty" ts_type:"string"`
	ReleasePromotedAt         *time.Time             `json:"release_promoted_at,omitempty" ts_type:"string"`
	ReleasePromotedByTokenID  *int64                 `json:"release_promoted_by_api_token_id,omitempty"`
}

type DeployApprovalRequestList struct {
	DeployApprovalRequests []DeployApprovalRequestResponse `json:"deploy_approval_requests" validate:"required"`
}
