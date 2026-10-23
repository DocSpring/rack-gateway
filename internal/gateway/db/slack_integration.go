package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/colcrypt"
)

// SlackIntegration represents a Slack workspace integration
type SlackIntegration struct {
	ID            int64  `json:"id"             validate:"required"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	// BotToken is the decrypted bot token. It is stored encrypted and never serialized.
	BotToken string `json:"-"`
	// BotTokenErr is set when the stored token can't be decrypted (another COLUMN_ENCRYPTION_KEY, or a
	// tampered value). BotToken is then empty; reconnecting Slack stores a new token.
	BotTokenErr                   error                  `json:"-"`
	ChannelActions                map[string]interface{} `json:"channel_actions"`
	AlertDeployApprovalsEnabled   bool                   `json:"alert_deploy_approvals_enabled"`
	AlertDeployApprovalsChannelID string                 `json:"alert_deploy_approvals_channel_id"`
	CreatedAt                     time.Time              `json:"created_at"`
	UpdatedAt                     time.Time              `json:"updated_at"`
	CreatedByUserID               *int64                 `json:"created_by_user_id,omitempty"`
	BotUserID                     string                 `json:"bot_user_id,omitempty"`
	Scope                         string                 `json:"scope,omitempty"`
}

// GetSlackIntegration retrieves the Slack integration (only one per gateway)
func (d *Database) GetSlackIntegration() (*SlackIntegration, error) {
	query := `
		SELECT
			id, workspace_id, workspace_name, bot_token_encrypted,
			channel_actions, alert_deploy_approvals_enabled, alert_deploy_approvals_channel_id,
			created_at, updated_at, created_by_user_id,
			bot_user_id, scope
		FROM slack_integration
		LIMIT 1
	`

	var si SlackIntegration
	var storedToken string
	var channelActionsJSON []byte

	err := d.db.QueryRow(query).Scan(
		&si.ID,
		&si.WorkspaceID,
		&si.WorkspaceName,
		&storedToken,
		&channelActionsJSON,
		&si.AlertDeployApprovalsEnabled,
		&si.AlertDeployApprovalsChannelID,
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

	// A token that can't be decrypted doesn't fail the lookup: the integration's settings stay visible and
	// it can be deleted or reconnected. Callers that need the token check BotTokenErr.
	token, err := d.cipher.Decrypt(storedToken, colcrypt.SlackBotTokenScope(si.WorkspaceID))
	if err != nil {
		si.BotTokenErr = fmt.Errorf("slack bot token: %w", err)
	} else {
		si.BotToken = token
	}

	return &si, nil
}

// ReplaceSlackIntegration connects a Slack workspace in place of the integration there was: a gateway has one. The
// old integration is deleted and the new one stored in one transaction, so a reconnect that fails keeps the old one.
// botToken is stored encrypted and bound to the workspace.
func (d *Database) ReplaceSlackIntegration(
	workspaceID, workspaceName, botToken, botUserID, scope string,
	channelActions map[string]interface{},
	createdByUserID *int64,
) (*SlackIntegration, error) {
	channelActionsJSON, err := json.Marshal(channelActions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal channel_actions: %w", err)
	}
	encryptedToken, err := d.cipher.Encrypt(botToken, colcrypt.SlackBotTokenScope(workspaceID))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt bot token: %w", err)
	}

	query := `
		INSERT INTO slack_integration (
			workspace_id, workspace_name, bot_token_encrypted,
			channel_actions, alert_deploy_approvals_enabled, alert_deploy_approvals_channel_id,
			created_by_user_id, bot_user_id, scope
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at
	`

	var si SlackIntegration
	// Reconnects take turns, so a second one replaces the first rather than failing on the one-row index.
	err = d.changeSlackIntegration(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM slack_integration"); err != nil {
			return fmt.Errorf("failed to remove the previous Slack integration: %w", err)
		}
		err := tx.QueryRowContext(
			ctx,
			query,
			workspaceID,
			workspaceName,
			encryptedToken,
			channelActionsJSON,
			false, // alert_deploy_approvals_enabled defaults to false
			"",    // alert_deploy_approvals_channel_id defaults to empty
			createdByUserID,
			botUserID,
			scope,
		).Scan(&si.ID, &si.CreatedAt, &si.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to store the Slack integration: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	si.WorkspaceID = workspaceID
	si.WorkspaceName = workspaceName
	si.BotToken = botToken
	si.ChannelActions = channelActions
	si.AlertDeployApprovalsEnabled = false
	si.AlertDeployApprovalsChannelID = ""
	si.CreatedByUserID = createdByUserID
	si.BotUserID = botUserID
	si.Scope = scope

	return &si, nil
}

// changeSlackIntegration runs fn in a transaction that holds AdvisoryLockSlackIntegration, as every change to the
// integration does. A reconnect replaces the integration's row: a change made alongside it without the lock could
// act on the row the reconnect deleted, miss the one it inserted, and report success (a disconnect that leaves the
// new bot connected).
func (d *Database) changeSlackIntegration(fn func(ctx context.Context, tx *sql.Tx) error) error {
	ctx := context.Background()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin a Slack integration change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockSlackIntegration); err != nil {
		return fmt.Errorf("failed to lock the Slack integration: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the Slack integration change: %w", err)
	}
	return nil
}

// ErrNoSlackIntegration is returned when there is no Slack integration to update.
var ErrNoSlackIntegration = errors.New("no Slack integration found to update")

// ErrSlackChannelsChanged is returned when the channel configuration changed after the caller read it.
var ErrSlackChannelsChanged = errors.New("the Slack channel configuration has changed")

// UpdateSlackIntegrationChannels replaces the channel configuration with channelActions while the integration is
// still integrationID and its configuration is still previous, the configuration the change was made to. An edit
// made to an older configuration (another tab or admin saved since), or to an integration that a reconnect has
// replaced, gets ErrSlackChannelsChanged instead of silently undoing that save or changing the new integration.
func (d *Database) UpdateSlackIntegrationChannels(
	integrationID int64,
	previous, channelActions map[string]interface{},
) error {
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return fmt.Errorf("failed to marshal previous channel_actions: %w", err)
	}
	channelActionsJSON, err := json.Marshal(channelActions)
	if err != nil {
		return fmt.Errorf("failed to marshal channel_actions: %w", err)
	}

	return d.changeSlackIntegration(func(ctx context.Context, tx *sql.Tx) error {
		var changed, missing bool
		err := tx.QueryRowContext(ctx, `
			WITH updated AS (
				UPDATE slack_integration
				SET channel_actions = $1, updated_at = NOW()
				WHERE id = $3 AND channel_actions = $2::jsonb
				RETURNING id
			)
			SELECT NOT EXISTS (SELECT 1 FROM updated), NOT EXISTS (SELECT 1 FROM slack_integration)
		`, channelActionsJSON, previousJSON, integrationID).Scan(&changed, &missing)
		if err != nil {
			return fmt.Errorf("failed to update channel_actions: %w", err)
		}
		switch {
		case missing:
			return ErrNoSlackIntegration
		case changed:
			return ErrSlackChannelsChanged
		}
		return nil
	})
}

// DeleteSlackIntegration disconnects Slack. Once it returns, no integration is connected, whatever reconnect ran
// alongside it (see changeSlackIntegration). Deleting when there's none isn't an error.
func (d *Database) DeleteSlackIntegration() error {
	return d.changeSlackIntegration(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM slack_integration"); err != nil {
			return fmt.Errorf("failed to delete the Slack integration: %w", err)
		}
		return nil
	})
}

// UpdateSlackAlertSettings updates the deploy approval alert configuration. It returns ErrNoSlackIntegration when
// Slack isn't connected.
func (d *Database) UpdateSlackAlertSettings(deployApprovalsEnabled bool, deployApprovalsChannelID string) error {
	return d.changeSlackIntegration(func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE slack_integration
			SET alert_deploy_approvals_enabled = $1,
			    alert_deploy_approvals_channel_id = $2,
			    updated_at = NOW()
		`, deployApprovalsEnabled, deployApprovalsChannelID)
		if err != nil {
			return fmt.Errorf("failed to update the Slack alert settings: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to update the Slack alert settings: %w", err)
		}
		if rows == 0 {
			return ErrNoSlackIntegration
		}
		return nil
	})
}
