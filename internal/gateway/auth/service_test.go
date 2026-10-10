package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

func TestAuthServiceAllowsCookieSession(t *testing.T) {
	database := dbtest.NewDatabase(t)

	if err := database.InitializeAdmin("user@example.com", "User"); err != nil {
		t.Fatalf("initialize admin: %v", err)
	}

	sessionManager := NewSessionManager(database, "test-secret", &StaticTTLProvider{TTL: time.Hour})

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

	if err := database.InitializeAdmin("user@example.com", "User"); err != nil {
		t.Fatalf("initialize admin: %v", err)
	}

	sessionManager := NewSessionManager(database, "test-secret", &StaticTTLProvider{TTL: time.Hour})

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

// A locked owner's API tokens stop working, including on routes that need no specific permission.
func TestAPITokenRejectedWhenOwnerLocked(t *testing.T) {
	database := dbtest.NewDatabase(t)

	owner, err := database.CreateUser("owner@example.com", "Owner", []string{"admin"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tokenResp, err := token.NewService(database).GenerateAPIToken(&token.APITokenRequest{
		Name: "CI", UserID: owner.ID, Permissions: token.DefaultCICDPermissions(),
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	svc := NewAuthService(token.NewService(database), database, nil)

	authenticates := func() bool {
		called := false
		next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true })
		req := httptest.NewRequest(http.MethodGet, "/api/v1/info", nil)
		req.Header.Set("Authorization", "Bearer "+tokenResp.Token)
		svc.Middleware(next).ServeHTTP(httptest.NewRecorder(), req)
		return called
	}

	if !authenticates() {
		t.Fatalf("token should authenticate while its owner is active")
	}
	if err := database.LockUser(owner.ID, "test lock", nil); err != nil {
		t.Fatalf("lock user: %v", err)
	}
	if authenticates() {
		t.Fatalf("token must be rejected once its owner is locked")
	}
}

// A failed API token attempt is audited with the client's IP and user agent.
func TestInvalidAPITokenAuditRecordsClient(t *testing.T) {
	database := dbtest.NewDatabase(t)
	svc := NewAuthService(token.NewService(database), database, nil)

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatalf("an invalid token must not authenticate")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/info", nil)
	req.RemoteAddr = "203.0.113.7:51234"
	req.Header.Set("User-Agent", "attacker-cli/1.0")
	req.Header.Set("Authorization", "Bearer rgw_not_a_real_token")
	svc.Middleware(next).ServeHTTP(httptest.NewRecorder(), req)

	logs, err := database.GetAuditLogs("", time.Time{}, 10)
	if err != nil {
		t.Fatalf("get audit logs: %v", err)
	}
	for _, entry := range logs {
		if entry.Action != "token.validate" {
			continue
		}
		if entry.IPAddress != "203.0.113.7" || entry.UserAgent != "attacker-cli/1.0" {
			t.Fatalf("audit row has ip %q, user agent %q", entry.IPAddress, entry.UserAgent)
		}
		return
	}
	t.Fatalf("no token.validate audit row in %d rows", len(logs))
}
