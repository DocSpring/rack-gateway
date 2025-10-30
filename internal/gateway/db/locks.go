package db

// Centralized advisory lock IDs used by the database package.
// These must be consistent across all processes interacting with the DB.

// AdvisoryLockAuditChain serializes audit chain appends.
const AdvisoryLockAuditChain int64 = 728443219

// AdvisoryLockMigration protects the audit migration from concurrent execution.
const AdvisoryLockMigration int64 = 728443218


