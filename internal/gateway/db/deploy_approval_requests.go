package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type rowScanner interface {
	Scan(dest ...interface{}) error
}

// Deploy approval request status constants
const (
	DeployApprovalRequestStatusPending  = "pending"
	DeployApprovalRequestStatusApproved = "approved"
	DeployApprovalRequestStatusRejected = "rejected"
	DeployApprovalRequestStatusExpired  = "expired"
	DeployApprovalRequestStatusDeployed = "deployed"
)

// Deploy approval request error variables
var (
	// ErrDeployApprovalRequestActive is returned when an active approval already exists for the same token and commit
	ErrDeployApprovalRequestActive = errors.New(
		"a deploy approval request is already pending or approved for this token and git commit",
	)
	// ErrDeployApprovalRequestNotFound is returned when a deploy approval request cannot be found
	ErrDeployApprovalRequestNotFound = errors.New("deploy approval request not found")
)

// DeployApprovalRequestConflictError wraps an existing approval request when a conflict is detected
type DeployApprovalRequestConflictError struct {
	Request *DeployApprovalRequest
}

func (e *DeployApprovalRequestConflictError) Error() string {
	return ErrDeployApprovalRequestActive.Error()
}

func (e *DeployApprovalRequestConflictError) Unwrap() error {
	return ErrDeployApprovalRequestActive
}

// CreateDeployApprovalRequest creates a new deploy approval request for the specified parameters.
func (d *Database) CreateDeployApprovalRequest(
	message, app, gitCommitHash, gitBranch, prURL string,
	ciMetadata []byte,
	createdByUserID int64,
	createdByAPITokenID *int64,
	targetAPITokenID int64,
	_ *int64,
) (*DeployApprovalRequest, error) {
	if err := validateDeployApprovalRequestInput(message, app, gitCommitHash, targetAPITokenID); err != nil {
		return nil, err
	}

	if err := d.checkDeployApprovalConflict(targetAPITokenID, gitCommitHash); err != nil {
		return nil, err
	}

	nullValues := buildDeployApprovalNullValues(createdByAPITokenID, gitBranch, prURL)

	id, err := d.insertDeployApprovalRequest(
		message, app, gitCommitHash, ciMetadata,
		createdByUserID, nullValues, targetAPITokenID,
	)
	if err != nil {
		return d.handleDeployApprovalInsertError(err, targetAPITokenID, gitCommitHash)
	}
	return d.GetDeployApprovalRequest(id)
}

func validateDeployApprovalRequestInput(
	message, app, gitCommitHash string,
	targetAPITokenID int64,
) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("message is required")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return fmt.Errorf("app is required")
	}
	gitCommitHash = strings.TrimSpace(gitCommitHash)
	if gitCommitHash == "" {
		return fmt.Errorf("git_commit_hash is required")
	}
	if targetAPITokenID <= 0 {
		return fmt.Errorf("target api token required")
	}
	return nil
}

func (d *Database) checkDeployApprovalConflict(tokenID int64, gitCommitHash string) error {
	existing, err := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitCommitHash,
		StatusFilter:  "any",
	})
	if err == nil && existing != nil {
		return &DeployApprovalRequestConflictError{Request: existing}
	}
	if err != nil && !errors.Is(err, ErrDeployApprovalRequestNotFound) {
		return fmt.Errorf("failed to check existing deploy approval requests: %w", err)
	}
	return nil
}

type deployApprovalNullValues struct {
	createdAPIToken sql.NullInt64
	gitBranch       sql.NullString
	prURL           sql.NullString
}

func buildDeployApprovalNullValues(
	createdByAPITokenID *int64,
	gitBranch, prURL string,
) deployApprovalNullValues {
	var result deployApprovalNullValues
	if createdByAPITokenID != nil {
		result.createdAPIToken = sql.NullInt64{Int64: *createdByAPITokenID, Valid: true}
	}
	if trimmed := strings.TrimSpace(gitBranch); trimmed != "" {
		result.gitBranch = sql.NullString{String: trimmed, Valid: true}
	}
	if trimmed := strings.TrimSpace(prURL); trimmed != "" {
		result.prURL = sql.NullString{String: trimmed, Valid: true}
	}
	return result
}

const deployApprovalInsertSQL = `INSERT INTO deploy_approval_requests
	(message, app, git_commit_hash, git_branch, pr_url, ci_metadata, status,
	created_by_user_id, created_by_api_token_id, target_api_token_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`

func (d *Database) insertDeployApprovalRequest(
	message, app, gitCommitHash string,
	ciMetadata []byte,
	createdByUserID int64,
	nulls deployApprovalNullValues,
	targetAPITokenID int64,
) (int64, error) {
	var id int64
	err := d.queryRow(
		deployApprovalInsertSQL,
		message,
		app,
		gitCommitHash,
		nulls.gitBranch,
		nulls.prURL,
		ciMetadata,
		DeployApprovalRequestStatusPending,
		createdByUserID,
		nulls.createdAPIToken,
		targetAPITokenID,
	).Scan(&id)
	return id, err
}

func (d *Database) handleDeployApprovalInsertError(
	err error,
	tokenID int64,
	gitCommitHash string,
) (*DeployApprovalRequest, error) {
	if !isUniqueConstraintViolation(err) {
		return nil, fmt.Errorf("failed to create deploy approval request: %w", err)
	}

	existing, fetchErr := d.FindDeployApprovalRequest(DeployApprovalLookup{
		TokenID:       tokenID,
		GitCommitHash: gitCommitHash,
		StatusFilter:  "any",
	})
	if fetchErr == nil && existing != nil {
		return nil, &DeployApprovalRequestConflictError{Request: existing}
	}
	return nil, ErrDeployApprovalRequestActive
}

func isUniqueConstraintViolation(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "idx_deploy_approval_requests_active_commit") ||
		strings.Contains(errMsg, "duplicate key value violates unique constraint")
}

// GetDeployApprovalRequest retrieves a deploy approval request by its internal ID
func (d *Database) GetDeployApprovalRequest(id int64) (*DeployApprovalRequest, error) {
	row := d.queryRow(deployApprovalRequestSelect+" WHERE dr.id = ?", id)
	return scanDeployApprovalRequest(row)
}

// GetDeployApprovalRequestByPublicID retrieves a deploy approval request by its public ID
func (d *Database) GetDeployApprovalRequestByPublicID(publicID string) (*DeployApprovalRequest, error) {
	row := d.queryRow(deployApprovalRequestSelect+" WHERE dr.public_id = ?", publicID)
	return scanDeployApprovalRequest(row)
}

// DeployApprovalLookup contains criteria for finding a deploy approval request
type DeployApprovalLookup struct {
	TokenID       int64
	GitCommitHash string
	App           string
	ReleaseID     string
	BuildID       string
	ProcessID     string
	StatusFilter  string // "any", "approved", or specific status
}

// FindDeployApprovalRequest finds a deploy approval request matching the given lookup criteria
func (d *Database) FindDeployApprovalRequest(lookup DeployApprovalLookup) (*DeployApprovalRequest, error) {
	clauses := []string{"dr.target_api_token_id = ?"}
	args := []interface{}{lookup.TokenID}

	if lookup.GitCommitHash != "" {
		clauses = append(clauses, "dr.git_commit_hash = ?")
		args = append(args, lookup.GitCommitHash)
	}
	if lookup.App != "" {
		clauses = append(clauses, "dr.app = ?")
		args = append(args, lookup.App)
	}
	if lookup.ReleaseID != "" {
		clauses = append(clauses, "dr.release_id = ?")
		args = append(args, lookup.ReleaseID)
	}
	if lookup.BuildID != "" {
		clauses = append(clauses, "dr.build_id = ?")
		args = append(args, lookup.BuildID)
	}
	if lookup.ProcessID != "" {
		clauses = append(clauses, "? = ANY(dr.process_ids)")
		args = append(args, lookup.ProcessID)
	}

	// Status filter
	if lookup.StatusFilter != "" && lookup.StatusFilter != "any" {
		clauses = append(clauses, "dr.status = ?")
		args = append(args, lookup.StatusFilter)
		// For approved status, also check expiration
		if lookup.StatusFilter == "approved" {
			clauses = append(clauses, "(dr.approval_expires_at IS NULL OR dr.approval_expires_at > NOW())")
		}
	}

	query := deployApprovalRequestSelect + " WHERE " + strings.Join(
		clauses,
		" AND ",
	) + " ORDER BY dr.created_at DESC LIMIT 1"
	row := d.queryRow(query, args...)
	return scanDeployApprovalRequest(row)
}

// GetDeployApprovalRequestForUser retrieves a deploy approval request by ID, verifying it belongs to the specified user
func (d *Database) GetDeployApprovalRequestForUser(id, userID int64) (*DeployApprovalRequest, error) {
	row := d.queryRow(deployApprovalRequestSelect+" WHERE dr.id = ? AND dr.created_by_user_id = ?", id, userID)
	return scanDeployApprovalRequest(row)
}

// DeployApprovalRequestListOptions contains options for listing deploy approval requests
type DeployApprovalRequestListOptions struct {
	Status   string
	Limit    int
	Offset   int
	OnlyOpen bool
	TokenID  int64
}

// ListDeployApprovalRequests returns a paginated list of deploy approval requests matching the given options
func (d *Database) ListDeployApprovalRequests(opts DeployApprovalRequestListOptions) ([]*DeployApprovalRequest, error) {
	clauses := []string{"TRUE"}
	args := []interface{}{}
	if opts.Status != "" {
		clauses = append(clauses, "dr.status = ?")
		args = append(args, opts.Status)
	}
	if opts.OnlyOpen {
		clauses = append(clauses, "dr.status IN ('pending','approved')")
	}
	if opts.TokenID > 0 {
		clauses = append(clauses, "dr.target_api_token_id = ?")
		args = append(args, opts.TokenID)
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := deployApprovalRequestSelect + " WHERE " + strings.Join(
		clauses,
		" AND ",
	) + " ORDER BY dr.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, opts.Offset)

	rows, err := d.query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list deploy approval requests: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*DeployApprovalRequest
	for rows.Next() {
		req, err := scanDeployApprovalRequest(rows)
		if err != nil {
			if errors.Is(err, ErrDeployApprovalRequestNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, req)
	}
	return out, nil
}

// updateApprovalStatus updates the status of a deploy approval request.
// whereClause should be "id = ?" or "public_id = ?".
// whereArg is the corresponding ID or publicID value.
func (d *Database) updateApprovalStatus(
	whereClause string,
	whereArg interface{},
	newStatus string,
	actorUserID int64,
	approvalExpiresAt *time.Time,
	notes string,
	allowedStatuses []string,
) error {
	trimmedNotes := strings.TrimSpace(notes)

	var whereColumn string
	switch whereClause {
	case "id = ?":
		whereColumn = "id"
	case "public_id = ?":
		whereColumn = "public_id"
	default:
		return fmt.Errorf("invalid where clause: %s", whereClause)
	}

	var updateSQL string
	var args []interface{}

	switch newStatus {
	case DeployApprovalRequestStatusApproved:
		if len(allowedStatuses) < 1 {
			return fmt.Errorf("allowedStatuses must include at least one allowed status")
		}
		if approvalExpiresAt == nil {
			return fmt.Errorf("approval expiration timestamp required for approved status")
		}
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, approved_by_user_id = ?, approved_at = NOW(),
             approval_expires_at = ?, approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status = ?`
		args = []interface{}{newStatus, actorUserID, *approvalExpiresAt, trimmedNotes, whereArg, allowedStatuses[0]}
	case DeployApprovalRequestStatusRejected:
		if len(allowedStatuses) < 2 {
			return fmt.Errorf("allowedStatuses must include two allowed statuses for rejection")
		}
		updateSQL = `UPDATE deploy_approval_requests
         SET status = ?, rejected_by_user_id = ?, rejected_at = NOW(), approval_notes = ?, updated_at = NOW()
         WHERE ` + whereColumn + ` = ? AND status IN (?, ?)`
		args = []interface{}{newStatus, actorUserID, trimmedNotes, whereArg, allowedStatuses[0], allowedStatuses[1]}
	default:
		return fmt.Errorf("unsupported status: %s", newStatus)
	}

	res, err := d.exec(updateSQL, args...)
	if err != nil {
		return fmt.Errorf("failed to update deploy approval request status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to update deploy approval request status: %w", err)
	}
	if rows == 0 {
		return ErrDeployApprovalRequestNotFound
	}
	return nil
}

// ApproveDeployApprovalRequest approves a pending deploy approval request by internal ID
func (d *Database) ApproveDeployApprovalRequest(
	id int64,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"id = ?",
		id,
		DeployApprovalRequestStatusApproved,
		approverUserID,
		&expiresAt,
		notes,
		[]string{DeployApprovalRequestStatusPending},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequest(id)
}

// ApproveDeployApprovalRequestByPublicID approves a pending deploy approval request by public ID
func (d *Database) ApproveDeployApprovalRequestByPublicID(
	publicID string,
	approverUserID int64,
	expiresAt time.Time,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"public_id = ?",
		publicID,
		DeployApprovalRequestStatusApproved,
		approverUserID,
		&expiresAt,
		notes,
		[]string{DeployApprovalRequestStatusPending},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}

// RejectDeployApprovalRequest rejects a pending or approved deploy approval request by internal ID
func (d *Database) RejectDeployApprovalRequest(
	id int64,
	approverUserID int64,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"id = ?",
		id,
		DeployApprovalRequestStatusRejected,
		approverUserID,
		nil,
		notes,
		[]string{DeployApprovalRequestStatusPending, DeployApprovalRequestStatusApproved},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequest(id)
}

// RejectDeployApprovalRequestByPublicID rejects a pending or approved deploy approval request by public ID
func (d *Database) RejectDeployApprovalRequestByPublicID(
	publicID string,
	approverUserID int64,
	notes string,
) (*DeployApprovalRequest, error) {
	err := d.updateApprovalStatus(
		"public_id = ?",
		publicID,
		DeployApprovalRequestStatusRejected,
		approverUserID,
		nil,
		notes,
		[]string{DeployApprovalRequestStatusPending, DeployApprovalRequestStatusApproved},
	)
	if err != nil {
		return nil, err
	}
	return d.GetDeployApprovalRequestByPublicID(publicID)
}

// UpdateDeployApprovalRequestObjectURL updates the object URL for an approved deploy approval request
func (d *Database) UpdateDeployApprovalRequestObjectURL(id int64, objectURL string) error {
	if strings.TrimSpace(objectURL) == "" {
		return fmt.Errorf("object url required")
	}
	res, err := d.exec(
		`UPDATE deploy_approval_requests
         SET object_url = ?, updated_at = NOW()
         WHERE id = ? AND status = ?`,
		objectURL,
		id,
		DeployApprovalRequestStatusApproved,
	)
	if err != nil {
		return fmt.Errorf("failed to update object url tracking: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to update object url tracking: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deployment approval not found or not approved")
	}
	return nil
}
