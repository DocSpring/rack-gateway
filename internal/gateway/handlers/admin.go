package handlers

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

type AdminHandler struct {
	rbac         rbac.RBACManager
	database     *db.Database
	tokenService *token.Service
	emailSender  email.Sender
	config       *config.Config
	rackCertMgr  *rackcert.Manager
	sessions     *auth.SessionManager
	mfaSettings  *db.MFASettings
	auditLogger  *audit.Logger
}

func NewAdminHandler(rbac rbac.RBACManager, database *db.Database, tokenService *token.Service, emailSender email.Sender, config *config.Config, rackCertMgr *rackcert.Manager, sessions *auth.SessionManager, mfaSettings *db.MFASettings, auditLogger *audit.Logger) *AdminHandler {
	return &AdminHandler{
		rbac:         rbac,
		database:     database,
		tokenService: tokenService,
		emailSender:  emailSender,
		config:       config,
		rackCertMgr:  rackCertMgr,
		sessions:     sessions,
		mfaSettings:  mfaSettings,
		auditLogger:  auditLogger,
	}
}
