package jobs

// Queue names for different job priorities
const (
	QueueSecurity      = "security"      // High priority security notifications
	QueueNotifications = "notifications" // Medium priority notifications
	QueueIntegrations  = "integrations"  // Low priority CI/GitHub integrations
)

// MaxAttemptsNotification is the maximum number of retry attempts for notification jobs.
// Retry schedule (exponential backoff): 1s, 2s, 4s, 8s, 16s, 32s, 1m, 2m, 4m, 8m, 16m
// Total time: ~42 minutes over 11 attempts
const MaxAttemptsNotification = 11
