package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SlackIntegration represents a Slack workspace integration
type SlackIntegration struct {
	ID                int64                  `json:"id"`
	WorkspaceID       string                 `json:"workspace_id"`
	WorkspaceName     string                 `json:"workspace_name"`
	BotTokenEncrypted string                 `json:"-"` // Never expose
	ChannelActions    map[string]interface{} `json:"channel_actions"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	CreatedByUserID   *int64                 `json:"created_by_user_id,omitempty"`
	BotUserID         string                 `json:"bot_user_id,omitempty"`
	Scope             string                 `json:"scope,omitempty"`
}

// GetSlackIntegration retrieves the Slack integration (only one per gateway)
func (d *Database) GetSlackIntegration() (*SlackIntegration, error) {
	query := `
		SELECT
			id, workspace_id, workspace_name, bot_token_encrypted,
			channel_actions, created_at, updated_at, created_by_user_id,
			bot_user_id, scope
		FROM slack_integration
		LIMIT 1
	`

	var si SlackIntegration
	var channelActionsJSON []byte

	err := d.db.QueryRow(query).Scan(
		&si.ID,
		&si.WorkspaceID,
		&si.WorkspaceName,
		&si.BotTokenEncrypted,
		&channelActionsJSON,
		&si.CreatedAt,
		&si.UpdatedAt,
		&si.CreatedByUserID,
		&si.BotUserID,
		&si.Scope,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(channelActionsJSON, &si.ChannelActions); err != nil {
		return nil, fmt.Errorf("failed to parse channel_actions: %w", err)
	}

	return &si, nil
}

// CreateSlackIntegration creates a new Slack integration
func (d *Database) CreateSlackIntegration(workspaceID, workspaceName, botTokenEncrypted, botUserID, scope string, channelActions map[string]interface{}, createdByUserID *int64) (*SlackIntegration, error) {
	channelActionsJSON, err := json.Marshal(channelActions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal channel_actions: %w", err)
	}

	query := `
		INSERT INTO slack_integration (
			workspace_id, workspace_name, bot_token_encrypted,
			channel_actions, created_by_user_id, bot_user_id, scope
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`

	var si SlackIntegration
	err = d.db.QueryRow(
		query,
		workspaceID,
		workspaceName,
		botTokenEncrypted,
		channelActionsJSON,
		createdByUserID,
		botUserID,
		scope,
	).Scan(&si.ID, &si.CreatedAt, &si.UpdatedAt)

	if err != nil {
		return nil, err
	}

	si.WorkspaceID = workspaceID
	si.WorkspaceName = workspaceName
	si.BotTokenEncrypted = botTokenEncrypted
	si.ChannelActions = channelActions
	si.CreatedByUserID = createdByUserID
	si.BotUserID = botUserID
	si.Scope = scope

	return &si, nil
}

// UpdateSlackIntegrationChannels updates the channel_actions configuration
func (d *Database) UpdateSlackIntegrationChannels(channelActions map[string]interface{}) error {
	channelActionsJSON, err := json.Marshal(channelActions)
	if err != nil {
		return fmt.Errorf("failed to marshal channel_actions: %w", err)
	}

	query := `
		UPDATE slack_integration
		SET channel_actions = $1, updated_at = NOW()
	`

	result, err := d.db.Exec(query, channelActionsJSON)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no Slack integration found to update")
	}

	return nil
}

// DeleteSlackIntegration removes the Slack integration
func (d *Database) DeleteSlackIntegration() error {
	query := `DELETE FROM slack_integration`
	_, err := d.db.Exec(query)
	return err
}
