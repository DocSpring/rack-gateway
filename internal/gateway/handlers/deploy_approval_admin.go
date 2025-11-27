package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobcircleci "github.com/DocSpring/rack-gateway/internal/gateway/jobs/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func parseDeployApprovalListOptions(c *gin.Context) (db.DeployApprovalRequestListOptions, bool) {
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
				return opts, false
			}
			opts.Limit = limit
		}
	}

	if offsetStr := strings.TrimSpace(c.Query("offset")); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			if offset < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be non-negative"})
				return opts, false
			}
			opts.Offset = offset
		}
	}

	return opts, true
}

// ListDeployApprovalRequests godoc
// @Summary List deploy approval requests
// @Description Returns a list of deploy approval requests with optional filtering
// @Tags Deploy Approvals
// @Produce json
// @Param status query string false "Filter by status (pending, approved, rejected, expired)"
// @Param only_open query boolean false "Only return open requests (pending status)"
// @Param limit query integer false "Maximum number of results"
// @Param offset query integer false "Offset for pagination"
// @Success 200 {object} DeployApprovalRequestList
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /deploy-approval-requests [get]
func (h *AdminHandler) ListDeployApprovalRequests(c *gin.Context) {
	if _, ok := h.requireDeployApprovalAccess(c, rbac.ActionApprove); !ok {
		return
	}

	opts, ok := parseDeployApprovalListOptions(c)
	if !ok {
		return
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

func (h *AdminHandler) getDeployApprovalExpiry() time.Time {
	// Get approval window from settings (default: 1 hour)
	windowMinutes := 60
	if h.settingsService != nil {
		minutes, err := h.settingsService.GetDeployApprovalWindowMinutes()
		if err != nil {
			log.Printf("WARN: Failed to get deploy_approval_window_minutes setting: %v", err)
		} else if minutes > 0 {
			windowMinutes = minutes
		}
	}

	window := time.Duration(windowMinutes) * time.Minute
	return time.Now().Add(window)
}

// ApproveDeployApprovalRequest godoc
// @Summary Approve a deploy approval request
// @Description Approves a deploy approval request and optionally triggers CircleCI job approval
// @Tags Deploy Approvals
// @Accept json
// @Produce json
// @Param id path string true "Deploy approval request public ID"
// @Param body body UpdateDeployApprovalRequestStatusRequest false "Approval notes"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /deploy-approval-requests/{id}/approve [post]
func (h *AdminHandler) ApproveDeployApprovalRequest(c *gin.Context) {
	input, ok := h.parseDeployApprovalStatusUpdateRequest(c)
	if !ok {
		return
	}

	expiresAt := h.getDeployApprovalExpiry()
	record, err := h.database.ApproveDeployApprovalRequestByPublicID(
		input.publicID,
		input.approver.ID,
		expiresAt,
		input.notes,
	)
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
		"notes":      strings.TrimSpace(input.notes),
		"message":    strings.TrimSpace(record.Message),
	})

	logDeployApprovalAudit(
		h.auditLogger,
		input.userEmail,
		input.approver.Name,
		audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), rbac.ActionApprove.String()),
		fmt.Sprintf("%d", record.ID),
		details,
		"success",
		http.StatusOK,
	)

	h.triggerCircleCIApprovalIfEnabled(c, record)
	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

// RejectDeployApprovalRequest godoc
// @Summary Reject a deploy approval request
// @Description Rejects a deploy approval request with optional notes
// @Tags Deploy Approvals
// @Accept json
// @Produce json
// @Param id path string true "Deploy approval request public ID"
// @Param body body UpdateDeployApprovalRequestStatusRequest false "Rejection notes"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /deploy-approval-requests/{id}/reject [post]
func (h *AdminHandler) RejectDeployApprovalRequest(c *gin.Context) {
	input, ok := h.parseDeployApprovalStatusUpdateRequest(c)
	if !ok {
		return
	}

	record, err := h.database.RejectDeployApprovalRequestByPublicID(input.publicID, input.approver.ID, input.notes)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"notes":   strings.TrimSpace(input.notes),
		"message": strings.TrimSpace(record.Message),
	})

	logDeployApprovalAudit(
		h.auditLogger,
		input.userEmail,
		input.approver.Name,
		audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), audit.ActionVerbReject),
		fmt.Sprintf("%d", record.ID),
		details,
		"success",
		http.StatusOK,
	)

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

// ExtendDeployApprovalRequest godoc
// @Summary Extend a deploy approval request expiry
// @Description Extends the expiry time for an approved deploy approval request
// @Tags Deploy Approvals
// @Accept json
// @Produce json
// @Param id path string true "Deploy approval request public ID"
// @Param body body handlers.UpdateDeployApprovalRequestStatusRequest false "Extension notes"
// @Success 200 {object} DeployApprovalRequestResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /deploy-approval-requests/{id}/extend [post]
func (h *AdminHandler) ExtendDeployApprovalRequest(c *gin.Context) {
	input, ok := h.parseDeployApprovalStatusUpdateRequest(c)
	if !ok {
		return
	}

	expiresAt := h.getDeployApprovalExpiry()
	record, err := h.database.ExtendDeployApprovalRequestExpiry(input.publicID, expiresAt)
	if err != nil {
		if errors.Is(err, db.ErrDeployApprovalRequestNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deploy approval request not found or not approved"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to extend deploy approval request"})
		return
	}

	details := auditDetails(map[string]string{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"message":    strings.TrimSpace(record.Message),
	})

	logDeployApprovalAudit(
		h.auditLogger,
		input.userEmail,
		input.approver.Name,
		audit.BuildAction(rbac.ResourceDeployApprovalRequest.String(), "extend"),
		fmt.Sprintf("%d", record.ID),
		details,
		"success",
		http.StatusOK,
	)

	c.JSON(http.StatusOK, toDeployApprovalRequestResponse(record))
}

func (h *AdminHandler) triggerCircleCIApprovalIfEnabled(c *gin.Context, record *db.DeployApprovalRequest) {
	if h.settingsService == nil || len(record.CIMetadata) == 0 {
		return
	}

	ciProvider, err := getAppSettingString(h.settingsService, record.App, settings.KeyCIProvider, "")
	if err != nil {
		log.Printf("WARN: Failed to get ci_provider setting: %v", err)
		return
	}

	if ciProvider != "circleci" {
		return
	}

	if !h.shouldAutoApproveCircleCI(record) {
		return
	}

	h.enqueueCircleCIApprovalJob(c, record)
}

func (h *AdminHandler) shouldAutoApproveCircleCI(record *db.DeployApprovalRequest) bool {
	autoApprove, err := getAppSettingBool(
		h.settingsService,
		record.App,
		settings.KeyCircleCIAutoApproveOnApproval,
		false,
	)
	if err != nil {
		log.Printf("WARN: Failed to get circleci_auto_approve_on_approval setting: %v", err)
		return false
	}

	if !autoApprove {
		return false
	}

	if h.config == nil || h.config.CircleCIToken == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no CircleCIToken configured")
		return false
	}

	return true
}

func (h *AdminHandler) enqueueCircleCIApprovalJob(c *gin.Context, record *db.DeployApprovalRequest) {
	var metadata map[string]interface{}
	if err := json.Unmarshal(record.CIMetadata, &metadata); err != nil {
		log.Printf("WARN: Failed to unmarshal CircleCI metadata: %v", err)
		return
	}

	approvalJobName, err := getAppSettingString(h.settingsService, record.App, settings.KeyCircleCIApprovalJobName, "")
	if err != nil {
		log.Printf("WARN: Failed to get circleci_approval_job_name setting: %v", err)
		return
	}

	if approvalJobName == "" {
		log.Printf("WARN: CircleCI auto-approve enabled but no approval_job_name configured for app %s", record.App)
		return
	}

	metadata["approval_job_name"] = approvalJobName

	circleciMetadata, err := circleci.ParseMetadata(metadata)
	if err != nil {
		log.Printf("WARN: Invalid CircleCI metadata: %v", err)
		return
	}

	if h.jobsClient != nil {
		_, err := h.jobsClient.Insert(c.Request.Context(), jobcircleci.ApproveJobArgs{
			CircleCIToken:   h.config.CircleCIToken,
			WorkflowID:      circleciMetadata.WorkflowID,
			PipelineNumber:  circleciMetadata.PipelineNumber,
			ApprovalJobName: circleciMetadata.ApprovalJobName,
		}, &river.InsertOpts{
			Queue:       jobs.QueueIntegrations,
			MaxAttempts: jobs.MaxAttemptsNotification,
		})
		if err != nil {
			log.Printf("ERROR: Failed to enqueue CircleCI approval job: %v", err)
		}
	}
}

// GetDeployApprovalRequestAuditLogs godoc
// @Summary Get audit logs for a deploy approval request
// @Description Returns audit logs for a specific deploy approval request
// @Tags Deploy Approvals
// @Produce json
// @Param id path string true "Deploy approval request public ID"
// @Param limit query integer false "Maximum number of results (default: 100)"
// @Success 200 {object} RawAuditLogsResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /deploy-approval-requests/{id}/audit-logs [get]
func (h *AdminHandler) GetDeployApprovalRequestAuditLogs(c *gin.Context) {
	if _, ok := h.requireDeployApprovalAccess(c, rbac.ActionApprove); !ok {
		return
	}

	publicID, ok := validatePublicID(c)
	if !ok {
		return
	}

	record, ok := loadDeployApprovalRequest(c, h.database, publicID)
	if !ok {
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
