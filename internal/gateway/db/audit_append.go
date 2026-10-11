package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// auditWriteTimeout bounds one audit append, including the wait for the chain lock.
const auditWriteTimeout = 10 * time.Second

// appendAuditEventSQL calls the only function allowed to insert audit events (see migrations).
const appendAuditEventSQL = `SELECT audit.append_audit_event(
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
	$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26
)`

// CreateAuditLog appends an audit event to the hash chain. The event is normalized to the values its row will
// hold, given an id and the next chain index under the chain lock, hashed (hash v2) and inserted in one
// transaction. On success, log holds the stored values.
func (d *Database) CreateAuditLog(log *AuditLog) error {
	if log == nil {
		return errors.New("audit log cannot be nil")
	}
	extractRequestIDFromDetails(log)
	row := newAuditChainRow(log)

	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	if err := d.appendAuditChainRow(ctx, row, auditWriterSecret()); err != nil {
		return err
	}
	applyStoredAuditRow(log, row)
	return nil
}

func (d *Database) appendAuditChainRow(ctx context.Context, row *AuditChainRow, secret []byte) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin audit log tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := linkAuditChainRow(ctx, tx, row); err != nil {
		return err
	}
	hash, err := ComputeAuditEventHash(secret, row)
	if err != nil {
		return fmt.Errorf("failed to hash audit event: %w", err)
	}
	row.EventHash = hash
	if err := insertAuditChainRow(ctx, tx, row); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit audit log tx: %w", err)
	}
	return nil
}

// linkAuditChainRow takes the chain lock (held until the transaction ends), then sets the row's chain index,
// previous hash and checkpoint from the latest event, and its id from the id sequence. The id and index are
// both hashed, so they're chosen here rather than by the insert.
func linkAuditChainRow(ctx context.Context, tx *sql.Tx, row *AuditChainRow) error {
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AdvisoryLockAuditChain); err != nil {
		return fmt.Errorf("failed to acquire audit chain lock: %w", err)
	}

	var latestIndex int64
	var latestHash, checkpointHash []byte
	var checkpointID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT chain_index, event_hash, checkpoint_id, checkpoint_hash
		FROM audit.audit_event
		ORDER BY chain_index DESC
		LIMIT 1`).Scan(&latestIndex, &latestHash, &checkpointID, &checkpointHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		row.ChainIndex = 0
		row.PreviousHash = nil
		row.CheckpointID = sql.NullString{}
		row.CheckpointHash = nil
	case err != nil:
		return fmt.Errorf("failed to get latest audit event: %w", err)
	default:
		row.ChainIndex = latestIndex + 1
		row.PreviousHash = latestHash
		row.CheckpointID = checkpointID
		row.CheckpointHash = checkpointHash
	}

	if err := tx.QueryRowContext(ctx, "SELECT nextval('audit.audit_event_id_seq')").Scan(&row.ID); err != nil {
		return fmt.Errorf("failed to allocate audit event id: %w", err)
	}
	return nil
}

func insertAuditChainRow(ctx context.Context, tx *sql.Tx, row *AuditChainRow) error {
	_, err := tx.ExecContext(ctx, appendAuditEventSQL,
		row.ID, row.ChainIndex, row.Timestamp, nullableBytes(row.PreviousHash), row.EventHash,
		row.CheckpointID, nullableBytes(row.CheckpointHash),
		row.UserEmail, row.UserName, row.APITokenID, row.APITokenName,
		row.ActionType, row.Action, row.Command, row.Resource, row.ResourceType, row.Details,
		row.RequestID, row.IPAddress, row.UserAgent, row.Status, row.RBACDecision,
		row.HTTPStatus, row.ResponseTimeMs, row.EventCount, row.DeployApprovalRequestID,
	)
	if err != nil {
		return fmt.Errorf("failed to append audit log: %w", err)
	}
	return nil
}

// nullableBytes passes a nil slice as SQL NULL.
func nullableBytes(value []byte) interface{} {
	if value == nil {
		return nil
	}
	return value
}
