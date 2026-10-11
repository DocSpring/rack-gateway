package proxy

import (
	"errors"
	"log"
	"net"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// errRackRequestFailed marks a forwarded request that failed after it was sent to the rack, e.g. a timeout. The rack
// may still be applying it.
var errRackRequestFailed = errors.New("failed to forward request")

func isPromoteRequest(r *http.Request, pth string) bool {
	return r.Method == http.MethodPost && rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}/promote")
}

// claimPromote claims the approval behind a promote, so two concurrent promotes with one approval can't both be
// forwarded. It runs after every other check, just before the promote is forwarded, so a refused promote never
// holds the claim.
func (h *Handler) claimPromote(r *http.Request, pth string, tracker *deployApprovalTracker) error {
	if tracker == nil || tracker.request == nil || h.database == nil || !isPromoteRequest(r, pth) {
		return nil
	}
	claimed, err := h.database.ClaimDeployApprovalPromote(tracker.request.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return &deployApprovalError{
			status:  http.StatusConflict,
			message: "a promote with this deploy approval request is already in progress",
		}
	}
	return nil
}

// releasePromoteClaimAfterFailure releases the approval's promote claim when the rack didn't apply the promote, so
// it can be retried. A successful promote already marked the approval deployed.
func (h *Handler) releasePromoteClaimAfterFailure(r *http.Request, pth string, statusCode int, forwardErr error) {
	if !isPromoteRequest(r, pth) || promoteMayHaveApplied(statusCode, forwardErr) {
		return
	}
	tracker := getDeployApprovalTracker(r.Context())
	if tracker == nil || tracker.request == nil || h.database == nil {
		return
	}
	if err := h.database.ReleaseDeployApprovalPromote(tracker.request.ID); err != nil {
		log.Printf("Failed to release deploy approval promote claim: %v", err)
	}
}

// promoteMayHaveApplied reports whether the rack may have applied a promote: it succeeded, or it was sent and then
// failed without a response (a timeout or dropped connection). The rack keeps applying a promote the gateway gave
// up on, so a retry would run a second promote alongside it; such a promote keeps its claim until the claim lapses.
func promoteMayHaveApplied(statusCode int, forwardErr error) bool {
	if forwardErr == nil {
		return statusCode >= 200 && statusCode < 300
	}
	var opErr *net.OpError
	if errors.As(forwardErr, &opErr) && opErr.Op == "dial" {
		return false
	}
	return errors.Is(forwardErr, errRackRequestFailed)
}
