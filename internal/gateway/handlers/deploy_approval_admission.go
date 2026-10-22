package handlers

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
)

// approvalCreationLimit limits how many deploy approval requests each API token (or user) creates: each one can
// cost several GitHub API calls on the gateway's GITHUB_TOKEN, and posts to Slack.
type approvalCreationLimit struct {
	creators *ratelimit.Limiter
	// retryAfter is the Retry-After value (seconds) for a request refused by the limit.
	retryAfter string
}

// newApprovalCreationLimit lets each creator make DEPLOY_APPROVAL_LIMIT_BURST requests (default 20), which come back
// at DEPLOY_APPROVAL_LIMIT_PER_HOUR (default 60).
func newApprovalCreationLimit() approvalCreationLimit {
	perHour := ratelimit.PositiveEnv("DEPLOY_APPROVAL_LIMIT_PER_HOUR", 60)
	return approvalCreationLimit{
		creators:   ratelimit.NewLimiter(perHour/3600, int(ratelimit.PositiveEnv("DEPLOY_APPROVAL_LIMIT_BURST", 20))),
		retryAfter: strconv.Itoa(int(math.Ceil(3600 / perHour))),
	}
}

// admitApprovalCreation refuses a new deploy approval request before GitHub verification calls GitHub: one that
// duplicates an open request (409, with that request), then one past its creator's limit (429). So a token allowed
// to create requests can't spend the gateway's GitHub quota by repeating them. The create checks for a duplicate
// again, in case one was created meanwhile. creatorTokenID is the API token that sent the request (nil when a
// user sent it); creatorUserID is its user.
func (h *APIHandler) admitApprovalCreation(
	c *gin.Context,
	targetTokenID int64,
	app, gitCommitHash string,
	creatorTokenID *int64,
	creatorUserID int64,
) bool {
	// An approval past its window (or a stale pending request) no longer counts as open, so expire them first:
	// otherwise one could block this request.
	if err := h.expireDeployApprovals(); err != nil {
		gtwlog.Errorf("deploy approvals: expiry sweep before create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deploy approval request"})
		return false
	}
	if err := h.database.CheckDeployApprovalConflict(targetTokenID, gitCommitHash, app); err != nil {
		h.handleCreateError(c, err, targetTokenID, gitCommitHash)
		return false
	}

	creator := fmt.Sprintf("user:%d", creatorUserID)
	if creatorTokenID != nil {
		creator = fmt.Sprintf("token:%d", *creatorTokenID)
	}
	if !h.approvalCreations.creators.Allow(creator) {
		gtwlog.Warnf("deploy approvals: %s is over its limit on new requests", creator)
		c.Header("Retry-After", h.approvalCreations.retryAfter)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many deploy approval requests; try again later"})
		return false
	}
	return true
}
