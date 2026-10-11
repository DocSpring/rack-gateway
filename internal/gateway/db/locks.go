package db

// Centralized advisory lock IDs used by the database package.
// These must be consistent across all processes interacting with the DB.

// AdvisoryLockAuditChain serializes audit chain appends.
const AdvisoryLockAuditChain int64 = 728443219

// AdvisoryLockAuditAnchor serializes audit anchor writes (see WithAuditAnchorLock).
const AdvisoryLockAuditAnchor int64 = 728443220

// AdvisoryLockMigration protects migrations from concurrent execution.
// Note: Test databases (rgw_test_*) skip this lock entirely since they're unique per test.
const AdvisoryLockMigration int64 = 728443218
