package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ListUserSessions godoc
// @Summary List active sessions for a user
// @Description Returns the active (non-revoked) web sessions for the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Success 200 {array} UserSessionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /users/{email}/sessions [get]
func (h *AdminHandler) ListUserSessions(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.list", email, "email is required", start, nil)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.list",
			email,
			"session management unavailable",
			start,
			nil,
		)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.list",
			email,
			"failed to load user",
			start,
			nil,
		)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.list", email, "user not found", start, nil)
		return
	}

	sessions, err := h.database.ListActiveSessionsByUser(user.ID)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.list",
			email,
			"failed to list sessions",
			start,
			nil,
		)
		return
	}

	result := make([]UserSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		entry := UserSessionResponse{
			ID:        sess.ID,
			CreatedAt: sess.CreatedAt.UTC().Format(time.RFC3339),
			LastSeen:  sess.LastSeenAt.UTC().Format(time.RFC3339),
			ExpiresAt: sess.ExpiresAt.UTC().Format(time.RFC3339),
			Channel:   sess.Channel,
		}
		if sess.IPAddress != "" {
			entry.IPAddress = sess.IPAddress
		}
		if sess.UserAgent != "" {
			entry.UserAgent = sess.UserAgent
		}
		if len(sess.Metadata) > 0 {
			var meta interface{}
			if err := json.Unmarshal(sess.Metadata, &meta); err == nil {
				entry.Metadata = meta
			} else {
				entry.Metadata = json.RawMessage(sess.Metadata)
			}
		}
		result = append(result, entry)
	}

	details := map[string]interface{}{"session_count": len(result)}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.list", email, start, details)
}

// RevokeUserSession godoc
// @Summary Revoke a user session
// @Description Revokes a single session for the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Param sessionID path int true "Session ID"
// @Success 200 {object} RevokeSessionResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/sessions/{sessionID}/revoke [post]
func (h *AdminHandler) RevokeUserSession(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(c, http.StatusBadRequest, "user.sessions.revoke", email, "email is required", start, nil)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke",
			email,
			"session management unavailable",
			start,
			nil,
		)
		return
	}

	sessionIDStr := strings.TrimSpace(c.Param("sessionID"))
	sessionID, err := strconv.ParseInt(sessionIDStr, 10, 64)
	if err != nil || sessionID <= 0 {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			"user.sessions.revoke",
			email,
			"invalid session id",
			start,
			map[string]interface{}{"session_id": sessionIDStr},
		)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke",
			email,
			"failed to load user",
			start,
			nil,
		)
		return
	}
	if user == nil {
		h.respondAuditError(
			c,
			http.StatusNotFound,
			"user.sessions.revoke",
			email,
			"user not found",
			start,
			map[string]interface{}{"session_id": sessionID},
		)
		return
	}

	session, err := h.database.GetUserSessionByID(sessionID)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke",
			email,
			"failed to load session",
			start,
			map[string]interface{}{"session_id": sessionID},
		)
		return
	}
	if session == nil || session.UserID != user.ID {
		h.respondAuditError(
			c,
			http.StatusNotFound,
			"user.sessions.revoke",
			email,
			"session not found",
			start,
			map[string]interface{}{"session_id": sessionID},
		)
		return
	}

	actorID := h.sessionActorID(c)
	revoked, err := h.sessions.RevokeByID(sessionID, actorID)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke",
			email,
			"failed to revoke session",
			start,
			map[string]interface{}{"session_id": sessionID},
		)
		return
	}

	result := RevokeSessionResponse{Revoked: revoked}
	details := map[string]interface{}{"session_id": sessionID, "revoked": revoked}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.revoke", email, start, details)
}

// RevokeAllUserSessions godoc
// @Summary Revoke all sessions for a user
// @Description Revokes every active session belonging to the specified user.
// @Tags Users
// @Produce json
// @Param email path string true "User email"
// @Success 200 {object} RevokeAllSessionsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /users/{email}/sessions/revoke_all [post]
func (h *AdminHandler) RevokeAllUserSessions(c *gin.Context) {
	start := time.Now()
	email := strings.TrimSpace(c.Param("email"))
	if email == "" {
		h.respondAuditError(
			c,
			http.StatusBadRequest,
			"user.sessions.revoke_all",
			email,
			"email is required",
			start,
			nil,
		)
		return
	}
	if h.sessions == nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke_all",
			email,
			"session management unavailable",
			start,
			nil,
		)
		return
	}

	user, err := h.database.GetUser(email)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke_all",
			email,
			"failed to load user",
			start,
			nil,
		)
		return
	}
	if user == nil {
		h.respondAuditError(c, http.StatusNotFound, "user.sessions.revoke_all", email, "user not found", start, nil)
		return
	}

	actorID := h.sessionActorID(c)
	revokedCount, err := h.sessions.RevokeAllForUser(user.ID, actorID)
	if err != nil {
		h.respondAuditError(
			c,
			http.StatusInternalServerError,
			"user.sessions.revoke_all",
			email,
			"failed to revoke sessions",
			start,
			nil,
		)
		return
	}

	result := RevokeAllSessionsResponse{RevokedCount: revokedCount}
	details := map[string]interface{}{"revoked_count": revokedCount}
	h.respondAuditSuccess(c, http.StatusOK, result, "user.sessions.revoke_all", email, start, details)
}

func (h *AdminHandler) sessionActorID(c *gin.Context) *int64 {
	if h == nil || h.database == nil {
		return nil
	}
	actorEmail := strings.TrimSpace(c.GetString("user_email"))
	if actorEmail == "" {
		return nil
	}
	actor, err := h.database.GetUser(actorEmail)
	if err != nil || actor == nil {
		return nil
	}
	id := actor.ID
	return &id
}
