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

// approvalAttemptsPerCreation is how many attempts to create a deploy approval request each creator may make for
// each request they may create: a duplicate (409) isn't a new request, but it's an attempt.
const approvalAttemptsPerCreation = 3

// approvalCreationLimit limits how many deploy approval requests each user creates, with their API tokens or
// themselves: each one can cost several GitHub API calls on the gateway's GITHUB_TOKEN, and posts to Slack. The
// limit is the user's, not each token's, so a user can't multiply it by creating tokens. attempts limits every
// attempt, refused or not, to approvalAttemptsPerCreation times that (see admitApprovalAttempt).
type approvalCreationLimit struct {
	creators *ratelimit.Limiter
	attempts *ratelimit.Limiter
	// retryAfter is the Retry-After value (seconds) for a request refused by the limit.
	retryAfter string
}

// newApprovalCreationLimit lets each creator make DEPLOY_APPROVAL_LIMIT_BURST requests (default 20), which come back
// at DEPLOY_APPROVAL_LIMIT_PER_HOUR (default 60).
func newApprovalCreationLimit() approvalCreationLimit {
	perHour := ratelimit.PositiveEnv("DEPLOY_APPROVAL_LIMIT_PER_HOUR", 60)
	burst := int(ratelimit.PositiveEnv("DEPLOY_APPROVAL_LIMIT_BURST", 20))
	return approvalCreationLimit{
		creators: ratelimit.NewLimiter(perHour/3600, burst),
		attempts: ratelimit.NewLimiter(
			approvalAttemptsPerCreation*perHour/3600, approvalAttemptsPerCreation*burst,
		),
		retryAfter: strconv.Itoa(int(math.Ceil(3600 / perHour))),
	}
}

// admitApprovalAttempt refuses (429) an attempt to create a deploy approval request past its creator's attempt
// limit, before the request is checked: the checks before the creation limit (the app's lookup on the rack, the
// expiry sweep, which holds the audit chain lock, and the duplicate lookup) cost something too, so a creator past
// their limits can't keep making the gateway do them.
func (h *APIHandler) admitApprovalAttempt(c *gin.Context, creatorUserID int64) bool {
	creator := fmt.Sprintf("user:%d", creatorUserID)
	if h.approvalCreations.attempts.Allow(creator) {
		return true
	}
	gtwlog.Warnf("deploy approvals: %s is over its limit on attempts to create requests", creator)
	h.refuseApprovalCreation(c)
	return false
}

// refuseApprovalCreation answers a deploy approval request refused by its creator's limits.
func (h *APIHandler) refuseApprovalCreation(c *gin.Context) {
	c.Header("Retry-After", h.approvalCreations.retryAfter)
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many deploy approval requests; try again later"})
}

// admitApprovalCreation refuses a new deploy approval request before GitHub verification calls GitHub: one that
// duplicates an open request (409, with that request), then one past its creator's limit (429). So a token allowed
// to create requests can't spend the gateway's GitHub quota by repeating them. The create checks for a duplicate
// again, in case one was created meanwhile. creatorUserID is the user who sent the request, or whose API token
// did.
func (h *APIHandler) admitApprovalCreation(
	c *gin.Context,
	targetTokenID int64,
	app, gitCommitHash string,
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
	if !h.approvalCreations.creators.Allow(creator) {
		gtwlog.Warnf("deploy approvals: %s is over its limit on new requests", creator)
		h.refuseApprovalCreation(c)
		return false
	}
	return true
}
