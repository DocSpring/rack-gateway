package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// auditChainColumns selects an AuditChainRow. The IP is read as text ("address/bits") so verification sees
// exactly what is stored.
const auditChainColumns = `id, chain_index, hash_version, "timestamp", previous_hash, event_hash,
	checkpoint_id, checkpoint_hash, user_email, user_name, api_token_id, api_token_name,
	action_type, action, command, resource, resource_type, details, request_id,
	ip_address::text, user_agent, status, rbac_decision, http_status, response_time_ms,
	event_count, deploy_approval_request_id`

// AuditChainHead describes the end of the chain, read in one snapshot.
type AuditChainHead struct {
	// Empty is true when there are no events (LastIndex and LastHash are then unset).
	Empty     bool
	LastIndex int64
	LastHash  []byte
	// HashV2From is the chain index of the first hash v2 event, if there is one.
	HashV2From sql.NullInt64
}

// AuditChainHead returns the latest event's index and hash and where hash v2 starts.
func (d *Database) AuditChainHead(ctx context.Context) (AuditChainHead, error) {
	var head AuditChainHead
	var lastIndex sql.NullInt64
	err := d.db.QueryRowContext(ctx, `
		SELECT latest.chain_index, latest.event_hash, cutover.first_v2
		FROM (SELECT MIN(chain_index) AS first_v2 FROM audit.audit_event WHERE hash_version >= 2) cutover
		LEFT JOIN (
			SELECT chain_index, event_hash FROM audit.audit_event ORDER BY chain_index DESC LIMIT 1
		) latest ON TRUE`).Scan(&lastIndex, &head.LastHash, &head.HashV2From)
	if err != nil {
		return head, fmt.Errorf("failed to read the audit chain head: %w", err)
	}
	head.Empty = !lastIndex.Valid
	head.LastIndex = lastIndex.Int64
	return head, nil
}

// AuditChainRows returns up to limit events with afterIndex < chain_index <= toIndex, in chain order.
func (d *Database) AuditChainRows(ctx context.Context, afterIndex, toIndex int64, limit int) ([]*AuditChainRow, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+auditChainColumns+`
		FROM audit.audit_event
		WHERE chain_index > $1 AND chain_index <= $2
		ORDER BY chain_index
		LIMIT $3`, afterIndex, toIndex, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to read audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []*AuditChainRow
	for rows.Next() {
		row, err := scanAuditChainRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read audit events: %w", err)
	}
	return result, nil
}

// AuditChainRowAt returns the event at a chain index, or nil if there is none.
func (d *Database) AuditChainRowAt(ctx context.Context, index int64) (*AuditChainRow, error) {
	return d.auditChainRow(ctx, `SELECT `+auditChainColumns+`
		FROM audit.audit_event WHERE chain_index = $1`, index)
}

// AuditChainRowBefore returns the event with the highest chain index below index, or nil if there is none.
func (d *Database) AuditChainRowBefore(ctx context.Context, index int64) (*AuditChainRow, error) {
	return d.auditChainRow(ctx, `SELECT `+auditChainColumns+`
		FROM audit.audit_event WHERE chain_index < $1
		ORDER BY chain_index DESC LIMIT 1`, index)
}

func (d *Database) auditChainRow(ctx context.Context, query string, index int64) (*AuditChainRow, error) {
	row, err := scanAuditChainRow(d.db.QueryRowContext(ctx, query, index))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

func scanAuditChainRow(scanner interface{ Scan(...interface{}) error }) (*AuditChainRow, error) {
	row := new(AuditChainRow)
	err := scanner.Scan(
		&row.ID, &row.ChainIndex, &row.HashVersion, &row.Timestamp, &row.PreviousHash, &row.EventHash,
		&row.CheckpointID, &row.CheckpointHash, &row.UserEmail, &row.UserName, &row.APITokenID, &row.APITokenName,
		&row.ActionType, &row.Action, &row.Command, &row.Resource, &row.ResourceType, &row.Details, &row.RequestID,
		&row.IPAddress, &row.UserAgent, &row.Status, &row.RBACDecision, &row.HTTPStatus, &row.ResponseTimeMs,
		&row.EventCount, &row.DeployApprovalRequestID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan audit event: %w", err)
	}
	row.Timestamp = row.Timestamp.UTC()
	row.IPAddress = storedAuditIP(row.IPAddress)
	return row, nil
}
