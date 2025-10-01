package db

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateProcess tracks a new process created via the gateway.
func (d *Database) CreateProcess(processID, app, releaseID string, createdByUserID, createdByAPITokenID *int64) error {
	query := `
		INSERT INTO processes (id, app, release_id, created_by_user_id, created_by_api_token_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := d.db.Exec(query, processID, app, releaseID, createdByUserID, createdByAPITokenID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to create process: %w", err)
	}
	return nil
}

// GetProcess retrieves a process by ID.
func (d *Database) GetProcess(processID string) (*Process, error) {
	query := `
		SELECT id, app, release_id, command, created_by_user_id, created_by_api_token_id,
		       deploy_approval_request_id, created_at, terminated_at
		FROM processes
		WHERE id = $1
	`
	var p Process
	var releaseID, command sql.NullString
	var createdByUserID, createdByAPITokenID, deployApprovalRequestID sql.NullInt64
	var terminatedAt sql.NullTime

	err := d.db.QueryRow(query, processID).Scan(
		&p.ID, &p.App, &releaseID, &command, &createdByUserID, &createdByAPITokenID,
		&deployApprovalRequestID, &p.CreatedAt, &terminatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}

	if releaseID.Valid {
		p.ReleaseID = releaseID.String
	}
	if command.Valid {
		p.Command = command.String
	}
	if createdByUserID.Valid {
		p.CreatedByUserID = &createdByUserID.Int64
	}
	if createdByAPITokenID.Valid {
		p.CreatedByAPITokenID = &createdByAPITokenID.Int64
	}
	if deployApprovalRequestID.Valid {
		p.DeployApprovalRequestID = &deployApprovalRequestID.Int64
	}
	if terminatedAt.Valid {
		p.TerminatedAt = &terminatedAt.Time
	}

	return &p, nil
}

// UpdateProcessCommand sets the command for a process (called on exec).
func (d *Database) UpdateProcessCommand(processID, command string, deployApprovalRequestID *int64) error {
	query := `
		UPDATE processes
		SET command = $1, deploy_approval_request_id = $2
		WHERE id = $3
	`
	_, err := d.db.Exec(query, command, deployApprovalRequestID, processID)
	if err != nil {
		return fmt.Errorf("failed to update process command: %w", err)
	}
	return nil
}

// MarkProcessTerminated marks a process as terminated.
func (d *Database) MarkProcessTerminated(processID string) error {
	query := `
		UPDATE processes
		SET terminated_at = $1
		WHERE id = $2
	`
	_, err := d.db.Exec(query, time.Now(), processID)
	if err != nil {
		return fmt.Errorf("failed to mark process as terminated: %w", err)
	}
	return nil
}

// GetProcessesByCreator retrieves all processes created by a specific user or API token.
func (d *Database) GetProcessesByCreator(userID *int64, apiTokenID *int64) ([]*Process, error) {
	query := `
		SELECT id, app, release_id, command, created_by_user_id, created_by_api_token_id,
		       deploy_approval_request_id, created_at, terminated_at
		FROM processes
		WHERE ($1::bigint IS NULL OR created_by_user_id = $1)
		  AND ($2::bigint IS NULL OR created_by_api_token_id = $2)
		ORDER BY created_at DESC
	`

	rows, err := d.db.Query(query, userID, apiTokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to query processes: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var processes []*Process
	for rows.Next() {
		var p Process
		var releaseID, command sql.NullString
		var createdByUserID, createdByAPITokenID, deployApprovalRequestID sql.NullInt64
		var terminatedAt sql.NullTime

		err := rows.Scan(
			&p.ID, &p.App, &releaseID, &command, &createdByUserID, &createdByAPITokenID,
			&deployApprovalRequestID, &p.CreatedAt, &terminatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan process: %w", err)
		}

		if releaseID.Valid {
			p.ReleaseID = releaseID.String
		}
		if command.Valid {
			p.Command = command.String
		}
		if createdByUserID.Valid {
			p.CreatedByUserID = &createdByUserID.Int64
		}
		if createdByAPITokenID.Valid {
			p.CreatedByAPITokenID = &createdByAPITokenID.Int64
		}
		if deployApprovalRequestID.Valid {
			p.DeployApprovalRequestID = &deployApprovalRequestID.Int64
		}
		if terminatedAt.Valid {
			p.TerminatedAt = &terminatedAt.Time
		}

		processes = append(processes, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate processes: %w", err)
	}

	return processes, nil
}
