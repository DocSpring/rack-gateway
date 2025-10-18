package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

func (h *AdminHandler) ListDeployApprovalRequests(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	opts := db.DeployApprovalRequestListOptions{}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		opts.Status = status
	}
	if onlyOpen := strings.TrimSpace(c.Query("only_open")); onlyOpen != "" {
		opts.OnlyOpen = onlyOpen == "true"
	}
	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			if limit < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be non-negative"})
				return
			}
			opts.Limit = limit
		}
	}
	if offsetStr := strings.TrimSpace(c.Query("offset")); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			if offset < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be non-negative"})
				return
			}
			opts.Offset = offset
		}
	}

	records, err := h.database.ListDeployApprovalRequests(opts)
	if err != nil {
		log.Printf("ERROR: failed to list deploy approval requests: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deploy approval requests"})
		return
	}

	responses := make([]DeployApprovalRequestResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, toDeployApprovalRequestResponse(record))
	}

	c.JSON(http.StatusOK, DeployApprovalRequestList{DeployApprovalRequests: responses})
}

func (h *AdminHandler) ApproveDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	var payload UpdateDeployApprovalRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	approver, err := h.database.GetUser(userEmail)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return
	}

	// Get approval window from settings (default: 15 minutes)
	windowMinutes := 15
	if h.settingsService != nil {
		minutes, err := h.settingsService.GetDeployApprovalWindowMinutes()
		if err != nil {
			log.Printf("WARN: Failed to get deploy_approval_window_minutes setting: %v", err)
		} else if minutes > 0 {
			windowMinutes = minutes
		}
	}

	window := time.Duration(windowMinutes) * time.Minute
	expiresAt := time.Now().Add(window)
	record, err := h.database.ApproveDeployApprovalRequestByPublicID(publicID, approver.ID, expiresAt, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"notes":      strings.TrimSpace(payload.Notes),
		"message":    strings.TrimSpace(record.Message),
	})

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), rbac.ActionApprove.String()),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	// Trigger CircleCI approval if configured and enabled
	if h.settingsService == nil || len(record.CIMetadata) == 0 {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Check if CI provider is CircleCI
	ciProvider, err := getAppSettingString(h.settingsService, record.App, settings.KeyCIProvider, "")
	if err != nil {
		log.Printf("WARN: Failed to get ci_provider setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if ciProvider != "circleci" {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	autoApprove, err := getAppSettingBool(h.settingsService, record.App, settings.KeyCircleCIAutoApproveOnApproval, false)
	if err != nil {
		log.Printf("WARN: Failed to get circleci_auto_approve_on_approval setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if !autoApprove {
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if h.config == nil || h.config.CircleCIToken == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no CircleCIToken configured")
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Parse CI metadata
	var metadata map[string]interface{}
	if err := json.Unmarshal(record.CIMetadata, &metadata); err != nil {
		log.Printf("WARN: Failed to unmarshal CircleCI metadata: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Get the approval job name from app settings
	approvalJobName, err := getAppSettingString(h.settingsService, record.App, settings.KeyCircleCIApprovalJobName, "")
	if err != nil {
		log.Printf("WARN: Failed to get circleci_approval_job_name setting: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	if approvalJobName == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no approval_job_name configured for app %s", record.App)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Override approval_job_name from settings if configured
	metadata["approval_job_name"] = approvalJobName

	// Validate and parse metadata
	circleciMetadata, err := circleci.ParseMetadata(metadata)
	if err != nil {
		log.Printf("WARN: Invalid CircleCI metadata: %v", err)
		c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
		return
	}

	// Trigger CircleCI approval in background (don't block response)
	go func() {
		client := circleci.NewClient(h.config.CircleCIToken)
		if err := client.ApproveJob(circleciMetadata.WorkflowID, circleciMetadata.ApprovalJobName); err != nil {
			log.Printf("ERROR: Failed to approve CircleCI job: %v", err)
		} else {
			log.Printf("INFO: Successfully approved CircleCI job %s in workflow %s", circleciMetadata.ApprovalJobName, circleciMetadata.WorkflowID)
		}
	}()

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

func (h *AdminHandler) RejectDeployApprovalRequest(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	var payload UpdateDeployApprovalRequestStatusRequest
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	approver, err := h.database.GetUser(userEmail)
	if err != nil || approver == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load approver"})
		return
	}

	record, err := h.database.RejectDeployApprovalRequestByPublicID(publicID, approver.ID, payload.Notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"notes":   strings.TrimSpace(payload.Notes),
		"message": strings.TrimSpace(record.Message),
	})

	_ = h.auditLogger.LogDBEntry(&db.AuditLog{
		UserEmail:    userEmail,
		UserName:     approver.Name,
		ActionType:   "gateway",
		Action:       audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), audit.ActionVerbReject),
		ResourceType: "deploy-approval-request",
		Resource:     fmt.Sprintf("%d", record.ID),
		Details:      details,
		Status:       "success",
		RBACDecision: "allow",
		HTTPStatus:   http.StatusOK,
	})

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

func (h *AdminHandler) GetDeployApprovalRequestAuditLogs(c *gin.Context) {
	if h == nil || h.database == nil || h.rbac == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "deploy approvals unavailable"})
		return
	}

	userEmail := strings.TrimSpace(c.GetString("user_email"))
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	allowed, err := h.rbac.Enforce(userEmail, rbac.ScopeGateway, rbac.ResourceDeployApprovalRequest, rbac.ActionApprove)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request id"})
		return
	}

	// Get the deploy approval request to verify it exists and get the internal ID
	record, err := h.database.GetDeployApprovalRequestByPublicID(publicID)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load deploy approval request"})
		return
	}

	limit := 100
	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	logs, err := h.database.GetAuditLogsByDeployApprovalRequestID(record.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch audit logs"})
		return
	}

	c.JSON(http.StatusOK, RawAuditLogsResponse{
		Logs:  logs,
		Total: len(logs),
		Page:  1,
		Limit: limit,
	})
}
