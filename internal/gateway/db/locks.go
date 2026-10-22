package db

// Centralized advisory lock IDs used by the database package.
// These must be consistent across all processes interacting with the DB.

// AdvisoryLockAuditChain serializes audit chain appends.
const AdvisoryLockAuditChain int64 = 728443219

// AdvisoryLockAuditAnchor serializes audit anchor writes (see WithAuditAnchorLock).
const AdvisoryLockAuditAnchor int64 = 728443220

// AdvisoryLockAppEnv is the class of the per-app env write locks (see LockAppEnv): the two-key lock's first key,
// with a hash of the app name as the second. It must fit in an int4.
const AdvisoryLockAppEnv int32 = 728443221

// AdvisoryLockSlackIntegration serializes changes to the Slack integration (see changeSlackIntegration).
const AdvisoryLockSlackIntegration int64 = 728443222

// AdvisoryLockSettings serializes settings changes (see inSettingsTx).
const AdvisoryLockSettings int64 = 728443223

// AdvisoryLockMigration protects migrations from concurrent execution.
// Note: Test databases (rgw_test_*) skip this lock entirely since they're unique per test.
const AdvisoryLockMigration int64 = 728443218
