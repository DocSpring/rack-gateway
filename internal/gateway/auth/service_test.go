package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestAuthServiceAllowsCookieSession(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	if err := database.InitializeAdmin("user@example.com", "User"); err != nil {
		t.Fatalf("initialize admin: %v", err)
	}

	sessionManager := NewSessionManager(database, "test-secret", time.Hour)

	user, err := database.GetUser("user@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user == nil {
		t.Fatalf("expected user to exist")
	}

	sessionToken, _, err := sessionManager.CreateSession(user, SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	svc := NewAuthService(nil, database, sessionManager)

	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		nextCalled = true
		user, ok := GetAuthUser(r.Context())
		if !ok {
			t.Fatalf("expected auth user in context")
		}
		if user.Email != "user@example.com" {
			t.Fatalf("unexpected user email: %s", user.Email)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/info", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: sessionToken})
	rw := httptest.NewRecorder()

	svc.Middleware(next).ServeHTTP(rw, req)

	if !nextCalled {
		t.Fatalf("next handler was not called; auth may have failed")
	}
	if rw.Code == http.StatusUnauthorized {
		t.Fatalf("expected successful auth, got 401")
	}
}

func TestValidateSessionRejectsLockedUser(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	if err := database.InitializeAdmin("user@example.com", "User"); err != nil {
		t.Fatalf("initialize admin: %v", err)
	}

	sessionManager := NewSessionManager(database, "test-secret", time.Hour)

	user, err := database.GetUser("user@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user == nil {
		t.Fatalf("expected user to exist")
	}

	sessionToken, session, err := sessionManager.CreateSession(user, SessionMetadata{Channel: "web"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Lock the user
	if err := database.LockUser(user.ID, "test lock", nil); err != nil {
		t.Fatalf("lock user: %v", err)
	}

	// Session validation should fail
	result, err := sessionManager.ValidateSession(sessionToken, "", "")
	if err == nil {
		t.Fatalf("expected validation to fail for locked user, got success")
	}
	if result != nil {
		t.Fatalf("expected nil result for locked user, got: %+v", result)
	}
	if err.Error() != "user locked" {
		t.Fatalf("expected 'user locked' error, got: %v", err)
	}

	// Session should be revoked
	revokedSession, err := database.GetUserSessionByID(session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if revokedSession.RevokedAt == nil {
		t.Fatalf("expected session to be revoked")
	}
}
