package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/debugredact"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// AuditBeforeActing makes every authenticated gateway request refuse to act without an audit record, reads
// included. Each request is recorded as attempted before its handler runs, and refused with 503 when the audit
// log can't take that entry. After the handler, the request's outcome is recorded too, unless the handler already
// wrote an entry for it.
//
// It must run after Authorize and the MFA checks, so only requests that are about to reach their handler are
// recorded as attempts (Authorize records denials). The MFA checks verify an inline MFA proof without saving what it
// changes in the session; that is saved here, once the attempt is written (saveInlineMFA).
func AuditBeforeActing(logger *audit.Logger, database *db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		spec, ok := rbac.LookupHTTPRoute(c.Request.Method, c.FullPath())
		authUser, authenticated := auth.GetAuthUser(c.Request.Context())
		if !ok || !authenticated || authUser == nil {
			// Authorize has refused anything that isn't an authenticated request to a route in the table.
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		start := time.Now()
		attempt := routeAuditEntry(c, spec, authUser)
		ctx, err := logger.RecordAttempt(c.Request, attempt)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": audit.RefusalMessage(attempt.Action)})
			return
		}
		c.Request = c.Request.WithContext(ctx)
		if !saveInlineMFA(c, database, authUser.Session) {
			recordOutcomeIfMissing(c, logger, spec, authUser, start)
			return
		}

		c.Next()

		recordOutcomeIfMissing(c, logger, spec, authUser, start)
	}
}

// recordOutcomeIfMissing writes the result of an attempted request whose handler wrote no audit entry for it
// (many handlers, such as the settings ones, write none), so every attempt has a recorded outcome. Its details
// include those the handler added with audit.Attempt.SetResultDetail.
func recordOutcomeIfMissing(
	c *gin.Context,
	logger *audit.Logger,
	spec rbac.RouteSpec,
	authUser *auth.User,
	start time.Time,
) {
	attempt := audit.AttemptFromContext(c.Request.Context())
	if attempt == nil || attempt.Results() > 0 {
		return
	}
	status := c.Writer.Status()
	result := routeAuditEntry(c, spec, authUser)
	if extra := attempt.ResultDetails(); len(extra) > 0 {
		result.Details = audit.WithDetails(result.Details, extra)
	}
	result.Status = logger.MapHTTPStatusToStatus(status)
	result.HTTPStatus = status
	result.ResponseTimeMs = int(time.Since(start).Milliseconds())
	_ = logger.LogRequestEntry(c.Request.Context(), result)
}

// routeAuditEntry is the audit entry for an authenticated request to a gateway route, before its outcome is
// known: named after the route (rbac.RouteSpec.AuditName), with the request path as its resource.
func routeAuditEntry(c *gin.Context, spec rbac.RouteSpec, authUser *auth.User) *db.AuditLog {
	name := spec.AuditName(c.Request.URL.Query())
	details := map[string]string{"method": c.Request.Method, "route": spec.Pattern}
	if raw := c.Request.URL.RawQuery; raw != "" {
		details["query"] = debugredact.RedactQuery(raw)
	}
	detailsJSON, _ := json.Marshal(details)
	return &db.AuditLog{
		UserEmail:    authUser.Email,
		UserName:     authUser.Name,
		APITokenID:   authUser.TokenID,
		APITokenName: strings.TrimSpace(authUser.TokenName),
		ActionType:   name.ActionType,
		Action:       name.Action,
		ResourceType: name.ResourceType,
		Resource:     strings.TrimPrefix(c.Request.URL.Path, "/api/v1"),
		Details:      string(detailsJSON),
		RequestID:    audit.RequestID(c.Request),
		IPAddress:    c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		RBACDecision: "allow",
		EventCount:   1,
	}
}
