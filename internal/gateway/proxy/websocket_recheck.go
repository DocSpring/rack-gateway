package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

const (
	// sessionRecheckPeriod is how often an open exec or log session's access is checked again.
	sessionRecheckPeriod = time.Minute
	// sessionRecheckTimeout ends a session whose recheck takes longer: its access can't be confirmed.
	sessionRecheckTimeout = 30 * time.Second
	// maxWebSocketSessionLifetime ends any exec or log session after this long, whatever its caller's access.
	maxWebSocketSessionLifetime = 12 * time.Hour
)

// errSessionAccessGone ends an open session whose caller can no longer start it.
var errSessionAccessGone = errors.New("access revoked")

// sessionAccess is what an open exec or log session's caller needed to start it, checked again while it runs.
type sessionAccess struct {
	authUser   *auth.User
	permission string
	// approvalID is the deploy approval that allowed an API token's session (0: none). It's checked instead of the
	// token's permissions, which don't include the step.
	approvalID int64
}

// sessionRecheck returns the check an open session on r runs every sessionRecheckPeriod (see recheck), or nil when
// there's nothing to check it against.
func (h *Handler) sessionRecheck(r *http.Request, authUser *auth.User, rackPath string) func() error {
	if h.database == nil || authUser == nil || authUser.DBUser == nil {
		return nil
	}
	resource, action, ok := rbac.MatchRackRoute("SOCKET", rackPath)
	if !ok {
		resource, action, ok = rbac.MatchRackRoute(http.MethodGet, rackPath)
	}
	if !ok {
		return nil
	}
	access := sessionAccess{authUser: authUser, permission: rbac.Convox(resource, action)}
	if tracker := getDeployApprovalTracker(r.Context()); tracker != nil && tracker.request != nil {
		access.approvalID = tracker.request.ID
	}
	return func() error { return h.recheck(access) }
}

// recheck reports why an open session must end, or nil while its caller could still start it: their user isn't
// locked or removed, their session (or API token) hasn't been revoked or expired, and they still hold the
// permission (or the deploy approval that allowed it hasn't been rejected). Checking doesn't count as activity: an
// open session doesn't keep its sign-in alive. An error looking anything up ends the session too.
func (h *Handler) recheck(access sessionAccess) error {
	user, err := h.database.GetUserByID(access.authUser.DBUser.ID)
	if err != nil {
		return err
	}
	if user == nil || user.LockedAt != nil {
		return fmt.Errorf("%w: the user was locked or removed", errSessionAccessGone)
	}
	if access.authUser.IsAPIToken {
		return h.recheckToken(access, user)
	}
	if err := h.recheckSignIn(access.authUser.Session); err != nil {
		return err
	}
	return h.recheckPermission(rbac.UserPrincipal(user), access.permission)
}

func (h *Handler) recheckToken(access sessionAccess, owner *db.User) error {
	if access.authUser.TokenID == nil {
		return fmt.Errorf("%w: no API token", errSessionAccessGone)
	}
	token, err := h.database.GetAPITokenByID(*access.authUser.TokenID)
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("%w: the API token was deleted", errSessionAccessGone)
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return fmt.Errorf("%w: the API token expired", errSessionAccessGone)
	}
	if access.approvalID == 0 {
		return h.recheckPermission(rbac.TokenPrincipal(owner, token.Permissions), access.permission)
	}
	if !tokenHasPermission(token.Permissions, rbac.Convox(rbac.ResourceDeploy, rbac.ActionDeployWithApproval)) {
		return fmt.Errorf("%w: the API token can no longer deploy with approval", errSessionAccessGone)
	}
	approval, err := h.database.GetDeployApprovalRequest(access.approvalID)
	if err != nil {
		return err
	}
	if approval == nil || approval.Status == db.DeployApprovalRequestStatusRejected {
		return fmt.Errorf("%w: the deploy approval request was rejected", errSessionAccessGone)
	}
	// An approval never allows more than its token's owner may do.
	return h.recheckPermission(rbac.UserPrincipal(owner), access.permission)
}

func (h *Handler) recheckSignIn(session *db.UserSession) error {
	if session == nil {
		return fmt.Errorf("%w: no session", errSessionAccessGone)
	}
	current, err := h.database.GetUserSessionByID(session.ID)
	if err != nil {
		return err
	}
	switch {
	case current == nil || current.RevokedAt != nil:
		return fmt.Errorf("%w: the session was revoked", errSessionAccessGone)
	case current.ExpiresAt.Before(time.Now()):
		return fmt.Errorf("%w: the session expired", errSessionAccessGone)
	}
	return nil
}

func (h *Handler) recheckPermission(principal rbac.Principal, permission string) error {
	allowed, err := h.rbacManager.Authorize(principal, permission)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: %s was removed", errSessionAccessGone, permission)
	}
	return nil
}
