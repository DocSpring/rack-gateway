package db

import (
	"fmt"
	"strings"
)

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
	var args []interface{}
	if opts.OnlyOpen {
		clauses = append(clauses,
			"(dr.status = 'pending' OR "+
				"(dr.status = 'approved' AND (dr.approval_expires_at IS NULL OR dr.approval_expires_at > NOW())))",
		)
	} else if opts.Status != "" {
		clauses = append(clauses, "dr.status = ?")
		args = append(args, opts.Status)
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
			return nil, err
		}
		out = append(out, req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during deploy approval requests iteration: %w", err)
	}
	return out, nil
}
