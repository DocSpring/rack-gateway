package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// requireJobsAccess checks authentication, authorization, and jobs client availability
func (h *AdminHandler) requireJobsAccess(c *gin.Context, action rbac.Action) (string, bool) {
	userEmail, ok := requireAuth(c, h.rbac, rbac.ResourceJob, action)
	if !ok {
		return "", false
	}

	if h.jobsClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "jobs system unavailable"})
		return "", false
	}

	return userEmail, true
}

// ListJobs godoc
// @Summary List background jobs
// @Description Returns a list of background jobs with optional filtering
// @Tags Jobs
// @Produce json
// @Param queue query string false "Filter by queue name"
// @Param state query string false "Filter by state (available, canceled, completed, discarded, pending, retryable, running, scheduled)"
// @Param kind query string false "Filter by job kind"
// @Param limit query integer false "Maximum number of results (default: 100, max: 1000)"
// @Success 200 {object} JobListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs [get]
func (h *AdminHandler) ListJobs(c *gin.Context) {
	if _, ok := h.requireJobsAccess(c, rbac.ActionList); !ok {
		return
	}

	// Parse filters
	limit := 100
	if limitStr := strings.TrimSpace(c.Query("limit")); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 1000 {
				parsed = 1000
			}
			limit = parsed
		}
	}

	queue := strings.TrimSpace(c.Query("queue"))
	state := strings.TrimSpace(c.Query("state"))
	kind := strings.TrimSpace(c.Query("kind"))

	// Build River query params using builder pattern
	params := river.NewJobListParams().First(limit)

	// Add filters
	if queue != "" {
		params = params.Queues(queue)
	}

	if state != "" {
		// Convert state string to River state
		switch state {
		case "available":
			params = params.States(rivertype.JobStateAvailable)
		case "canceled":
			params = params.States(rivertype.JobStateCancelled)
		case "completed":
			params = params.States(rivertype.JobStateCompleted)
		case "discarded":
			params = params.States(rivertype.JobStateDiscarded)
		case "pending":
			params = params.States(rivertype.JobStatePending)
		case "retryable":
			params = params.States(rivertype.JobStateRetryable)
		case "running":
			params = params.States(rivertype.JobStateRunning)
		case "scheduled":
			params = params.States(rivertype.JobStateScheduled)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state filter"})
			return
		}
	}

	if kind != "" {
		params = params.Kinds(kind)
	}

	// Fetch jobs from River
	result, err := h.jobsClient.JobList(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list jobs"})
		return
	}

	// Convert to response format
	responses := make([]JobResponse, 0, len(result.Jobs))
	for _, job := range result.Jobs {
		responses = append(responses, toJobResponse(job))
	}

	c.JSON(http.StatusOK, JobListResponse{
		Jobs:  responses,
		Count: len(responses),
		Limit: limit,
	})
}

// GetJob godoc
// @Summary Get a background job
// @Description Returns details of a specific background job by ID
// @Tags Jobs
// @Produce json
// @Param id path integer true "Job ID"
// @Success 200 {object} JobResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /jobs/{id} [get]
func (h *AdminHandler) GetJob(c *gin.Context) {
	if _, ok := h.requireJobsAccess(c, rbac.ActionRead); !ok {
		return
	}

	idStr := strings.TrimSpace(c.Param("id"))
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id is required"})
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job id"})
		return
	}

	job, err := h.jobsClient.JobGet(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch job"})
		}
		return
	}

	c.JSON(http.StatusOK, toJobResponse(job))
}

func toJobResponse(job *rivertype.JobRow) JobResponse {
	var lastError string
	if len(job.Errors) > 0 {
		lastError = job.Errors[len(job.Errors)-1].Error
	}

	// Decode args from byte array to JSON
	// The args are stored as bytes in the database, we just pass them through as-is
	// and let JSON marshal handle the conversion
	var args json.RawMessage
	if len(job.EncodedArgs) > 0 {
		// River stores args as JSON bytes, we just return them as JSON
		args = job.EncodedArgs
	}

	return JobResponse{
		ID:          job.ID,
		State:       string(job.State),
		Queue:       job.Queue,
		Kind:        job.Kind,
		Args:        args,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
		CreatedAt:   job.CreatedAt,
		ScheduledAt: job.ScheduledAt,
		AttemptedAt: job.AttemptedAt,
		FinalizedAt: job.FinalizedAt,
		Errors:      formatJobErrors(job.Errors),
		LastError:   lastError,
	}
}

func formatJobErrors(errors []rivertype.AttemptError) []JobErrorResponse {
	result := make([]JobErrorResponse, 0, len(errors))
	for _, e := range errors {
		result = append(result, JobErrorResponse{
			Attempt: e.Attempt,
			Error:   e.Error,
			At:      e.At,
		})
	}
	return result
}

// Response types
type JobListResponse struct {
	Jobs  []JobResponse `json:"jobs"`
	Count int           `json:"count"` // Number of jobs in this response (not total count across all pages)
	Limit int           `json:"limit"`
}

type JobResponse struct {
	ID          int64              `json:"id"`
	State       string             `json:"state"`
	Queue       string             `json:"queue"`
	Kind        string             `json:"kind"`
	Args        json.RawMessage    `json:"args,omitempty"`
	Attempt     int                `json:"attempt"`
	MaxAttempts int                `json:"max_attempts"`
	CreatedAt   time.Time          `json:"created_at"`
	ScheduledAt time.Time          `json:"scheduled_at"`
	AttemptedAt *time.Time         `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time         `json:"finalized_at,omitempty"`
	Errors      []JobErrorResponse `json:"errors,omitempty"`
	LastError   string             `json:"last_error,omitempty"`
}

type JobErrorResponse struct {
	Attempt int       `json:"attempt"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
}
