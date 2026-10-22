package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rackcert"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// An approval uploads one archive, creates one build from it and promotes its release once. Each of those steps
// claims the approval after its permission checks and before it's forwarded, so two concurrent requests under one
// approval can't both reach the rack. A step that didn't complete releases its claim (releaseDeployApprovalClaims),
// so a request the gateway or the rack refused doesn't block a retry.

// errRackRequestFailed marks a forwarded request that failed after it was sent to the rack, e.g. a timeout. The rack
// may still be applying it.
var errRackRequestFailed = errors.New("failed to forward request")

func isPromoteRequest(r *http.Request, pth string) bool {
	return r.Method == http.MethodPost && rbac.KeyMatch3(pth, "/apps/{app}/releases/{id}/promote")
}

func isBuildCreate(r *http.Request, pth string) bool {
	return r.Method == http.MethodPost && rbac.KeyMatch3("/"+strings.TrimLeft(pth, "/"), "/apps/{app}/builds")
}

// uploadObjectURL is the URL the rack gives an archive uploaded to pth (/apps/{app}/objects/tmp/{name}).
func uploadObjectURL(pth string) string {
	return fmt.Sprintf("object://%s/%s", extractAppFromPath(pth), uploadObjectKey(pth))
}

// uploadObjectKey is the key of an archive uploaded to pth (/apps/{app}/objects/tmp/{name}).
func uploadObjectKey(pth string) string {
	return "tmp/" + pth[strings.LastIndex(pth, "/")+1:]
}

func hasApproval(tracker *deployApprovalTracker) bool {
	return tracker != nil && tracker.request != nil
}

// claimDeployApprovalStep claims the approval step of a request that's about to be forwarded. Every upload also
// claims its object key, which is never released (db.ClaimObjectUpload): the rack would overwrite an object uploaded
// again, and a build would then use an archive the gateway never checked.
func (h *Handler) claimDeployApprovalStep(r *http.Request, pth string, tracker *deployApprovalTracker) error {
	if h.database == nil {
		return nil
	}
	switch {
	case isObjectUpload(r, pth):
		return h.claimObjectUpload(pth, tracker)
	case isBuildCreate(r, pth):
		return h.claimBuild(tracker)
	default:
		return h.claimPromote(r, pth, tracker)
	}
}

func (h *Handler) claimObjectUpload(pth string, tracker *deployApprovalTracker) error {
	objectURL := uploadObjectURL(pth)
	claimed, err := h.database.ClaimObjectUpload(extractAppFromPath(pth), uploadObjectKey(pth))
	if err != nil {
		return err
	}
	if !claimed {
		return &deployApprovalError{
			status:  http.StatusConflict,
			message: fmt.Sprintf("%s has already been uploaded: upload the archive to a new key", objectURL),
		}
	}
	if !hasApproval(tracker) {
		return nil
	}
	claimed, err = h.database.ClaimDeployApprovalObjectUpload(tracker.request.ID, objectURL)
	if errors.Is(err, db.ErrDeployApprovalObjectBound) {
		return objectBoundError(objectURL)
	}
	if err != nil {
		return err
	}
	if !claimed {
		return &deployApprovalError{
			status:  http.StatusConflict,
			message: "an archive has already been uploaded (or is being uploaded) for this deploy approval request",
		}
	}
	return nil
}

func objectBoundError(objectURL string) error {
	return &deployApprovalError{
		status:  http.StatusConflict,
		message: fmt.Sprintf("%s holds a deploy approval request's archive and can't be replaced", objectURL),
	}
}

func (h *Handler) claimBuild(tracker *deployApprovalTracker) error {
	if !hasApproval(tracker) {
		return nil
	}
	claimed, err := h.database.ClaimDeployApprovalBuild(tracker.request.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return &deployApprovalError{
			status: http.StatusConflict,
			message: "a build has already been started for this deploy approval request, or its archive " +
				"hasn't finished uploading",
		}
	}
	return nil
}

func (h *Handler) claimPromote(r *http.Request, pth string, tracker *deployApprovalTracker) error {
	if !hasApproval(tracker) || !isPromoteRequest(r, pth) {
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

// releaseDeployApprovalClaims releases the claim of a step that didn't complete, so CI can retry it. A completed
// upload or build was recorded while its response was processed, and keeps its claim. A promote keeps its claim
// while the rack may still be applying it (a successful promote already marked the approval deployed).
func (h *Handler) releaseDeployApprovalClaims(r *http.Request, pth string, statusCode int, forwardErr error) {
	tracker := getDeployApprovalTracker(r.Context())
	if !hasApproval(tracker) || h.database == nil {
		return
	}
	var err error
	switch {
	case isObjectUpload(r, pth):
		err = h.database.ReleaseDeployApprovalObjectUpload(tracker.request.ID, uploadObjectURL(pth))
	case isBuildCreate(r, pth):
		err = h.database.ReleaseDeployApprovalBuild(tracker.request.ID)
	case isPromoteRequest(r, pth) && !promoteMayHaveApplied(statusCode, forwardErr):
		err = h.database.ReleaseDeployApprovalPromote(tracker.request.ID)
	}
	if err != nil {
		gtwlog.Errorf("failed to release deploy approval claim: %v", err)
	}
}

// promoteMayHaveApplied reports whether the rack may have applied a promote. The rack keeps applying a promote the
// gateway gave up on, so a retry would run a second promote alongside it; such a promote keeps its claim, and CI
// needs a new approval. Only a promote that certainly didn't apply releases it: the rack answered with an error
// status, or the request never reached the rack. Any other failure, such as a timeout or a response body that broke
// off after a 2xx status (which reports no status), may have applied.
func promoteMayHaveApplied(statusCode int, forwardErr error) bool {
	if statusCode >= 300 {
		return false
	}
	return forwardErr == nil || !neverReachedRack(forwardErr)
}

// neverReachedRack reports whether a forwarding error happened before the request was sent: the connection or its
// TLS handshake (the rack's certificate didn't match the pinned one) failed, or the gateway refused the body.
func neverReachedRack(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	if _, mismatch := rackcert.AsFingerprintMismatch(err); mismatch {
		return true
	}
	return errors.Is(err, errBodyTooLarge)
}
