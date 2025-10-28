package handlers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	rbac            rbac.RBACManager
	database        *db.Database
	config          *config.Config
	rackCertManager *rackcert.Manager
	mfaSettings     *db.MFASettings
	auditLogger     *audit.Logger
	settingsService *settings.Service
}

var (
	errRackNotConfigured = errors.New("rack not configured")
	errRackTLSConfig     = errors.New("rack tls configuration failed")
)

func NewAPIHandler(
	rbac rbac.RBACManager,
	database *db.Database,
	config *config.Config,
	rackCertManager *rackcert.Manager,
	mfaSettings *db.MFASettings,
	auditLogger *audit.Logger,
	settingsService *settings.Service,
) *APIHandler {
	return &APIHandler{
		rbac:            rbac,
		database:        database,
		config:          config,
		rackCertManager: rackCertManager,
		mfaSettings:     mfaSettings,
		auditLogger:     auditLogger,
		settingsService: settingsService,
	}
}

func (h *APIHandler) primaryRack() (config.RackConfig, bool) {
	if h == nil || h.config == nil {
		return config.RackConfig{}, false
	}
	if rc, ok := h.config.Racks["default"]; ok && rc.Enabled {
		return rc, true
	}
	if rc, ok := h.config.Racks["local"]; ok && rc.Enabled {
		return rc, true
	}
	return config.RackConfig{}, false
}

func (h *APIHandler) stepUpWindow() time.Duration {
	if h.mfaSettings != nil && h.mfaSettings.StepUpWindowMinutes > 0 {
		return time.Duration(h.mfaSettings.StepUpWindowMinutes) * time.Minute
	}
	return 10 * time.Minute
}

func (h *APIHandler) rackContext(ctx context.Context) (config.RackConfig, *tls.Config, error) {
	rc, ok := h.primaryRack()
	if !ok || strings.TrimSpace(rc.URL) == "" || strings.TrimSpace(rc.APIKey) == "" {
		return config.RackConfig{}, nil, errRackNotConfigured
	}

	var tlsCfg *tls.Config
	if h.rackCertManager != nil {
		cfg, err := h.rackCertManager.TLSConfig(ctx)
		if err != nil {
			return config.RackConfig{}, nil, fmt.Errorf("%w: %v", errRackTLSConfig, err)
		}
		tlsCfg = cfg
	}

	return rc, tlsCfg, nil
}

func (h *APIHandler) acquireRackContext(c *gin.Context) (config.RackConfig, *tls.Config, bool) {
	rackConfig, tlsCfg, err := h.rackContext(c.Request.Context())
	if err == nil {
		return rackConfig, tlsCfg, true
	}

	switch {
	case errors.Is(err, errRackNotConfigured):
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rack not configured"})
	case errors.Is(err, errRackTLSConfig):
		log.Printf(`{"level":"error","event":"rack_tls_config_error","message":%q}`, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack TLS"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to prepare rack connection"})
	}

	return config.RackConfig{}, nil, false
}
