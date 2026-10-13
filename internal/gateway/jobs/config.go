package jobs

// Queue names for different job priorities
const (
	QueueSecurity      = "security"      // High priority security notifications
	QueueNotifications = "notifications" // Medium priority notifications
	QueueIntegrations  = "integrations"  // Low priority CI/GitHub integrations
)

// MaxAttemptsNotification is the maximum number of retry attempts for notification jobs.
// Retries use River's default backoff of attempt^4 seconds (±10% jitter): 1s, 16s, 81s, ~4m, ~10m, ~22m, ~40m,
// ~68m, ~109m, ~167m. Total time: about 7 hours over 11 attempts.
const MaxAttemptsNotification = 11
