package handlers

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// APIHandler handles API requests for Convox apps including environment variable management
type APIHandler struct {
	rbac            rbac.Manager
	database        *db.Database
	config          *config.Config
	rackCertManager *rackcert.Manager
	mfaSettings     *db.MFASettings
	auditLogger     *audit.Logger
	settingsService *settings.Service
	slackNotifier   SlackNotifier
	jobsClient      *jobs.Client
}

// SlackNotifier defines the interface for sending Slack notifications
type SlackNotifier interface {
	NotifyDeployApprovalCreated(req *db.DeployApprovalRequest, gatewayDomain string) error
}

// NewAPIHandler creates a new API handler with the provided dependencies
func NewAPIHandler(
	rbacManager rbac.Manager,
	database *db.Database,
	cfg *config.Config,
	rackCertManager *rackcert.Manager,
	mfaSettings *db.MFASettings,
	auditLogger *audit.Logger,
	settingsService *settings.Service,
	slackNotifier SlackNotifier,
	jobsClient *jobs.Client,
) *APIHandler {
	return &APIHandler{
		rbac:            rbacManager,
		database:        database,
		config:          cfg,
		rackCertManager: rackCertManager,
		mfaSettings:     mfaSettings,
		auditLogger:     auditLogger,
		settingsService: settingsService,
		slackNotifier:   slackNotifier,
		jobsClient:      jobsClient,
	}
}
