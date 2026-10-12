package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// Adding an MFA method is free for a user enrolling their first factor, and needs step-up once they have one:
// otherwise a session that never verified a factor could add its own and step up with it.
func TestEnforceMFARequirements_StepUpOnceEnrolled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	mfaService, settingsService, sessionManager := setupMFAHelpers(t, database)
	const endpoint = "/api/v1/auth/mfa/enroll/totp/start"

	newUser := func(t *testing.T, email string, enrolled bool) *db.User {
		t.Helper()
		user, err := database.CreateUser(email, "User", []string{"viewer"})
		require.NoError(t, err)
		require.NoError(t, database.SetUserMFAEnrolled(user.ID, enrolled))
		return user
	}
	serve := func(user *db.User, session *db.UserSession) *httptest.ResponseRecorder {
		middleware := EnforceMFARequirements(mfaService, database, settingsService)
		router := buildStepUpRouter(endpoint, session, user, middleware, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, endpoint, nil))
		return w
	}

	t.Run("a first factor needs no step-up", func(t *testing.T) {
		user := newUser(t, "first-factor@example.com", false)
		require.Equal(t, http.StatusOK, serve(user, confirmSession(t, sessionManager, user)).Code)
	})

	t.Run("another factor needs step-up", func(t *testing.T) {
		user := newUser(t, "another-factor@example.com", true)
		w := serve(user, confirmSession(t, sessionManager, user))
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Contains(t, w.Body.String(), "mfa_step_up_required")
	})

	t.Run("a recent step-up covers another factor", func(t *testing.T) {
		user := newUser(t, "stepped-up@example.com", true)
		session := confirmSession(t, sessionManager, user)
		steppedUpAt := time.Now()
		session.RecentStepUpAt = &steppedUpAt
		require.Equal(t, http.StatusOK, serve(user, session).Code)
	})
}
