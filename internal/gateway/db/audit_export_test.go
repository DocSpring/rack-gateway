package db

import "context"

// NewAuditChainRowForTest returns the stored form of log, before it's linked into the chain.
func NewAuditChainRowForTest(log *AuditLog) *AuditChainRow {
	return newAuditChainRow(log)
}

// InsertAuditChainRowForTest hashes row and passes it to audit.append_audit_event as it is, without taking the
// chain lock or linking it to the latest event, so tests can check what the function refuses.
func (d *Database) InsertAuditChainRowForTest(ctx context.Context, row *AuditChainRow, secret []byte) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if row.EventHash, err = ComputeAuditEventHash(secret, row); err != nil {
		return err
	}
	if err := insertAuditChainRow(ctx, tx, row); err != nil {
		return err
	}
	return tx.Commit()
}
