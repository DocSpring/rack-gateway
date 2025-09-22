package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
)

func TestAuthServiceAllowsCookieJWT(t *testing.T) {
	database := dbtest.NewDatabase(t)
	t.Cleanup(func() { dbtest.Reset(t, database) })

	if err := database.InitializeAdmin("user@example.com", "User"); err != nil {
		t.Fatalf("initialize admin: %v", err)
	}

	manager := NewJWTManager("test-secret", time.Hour)
	token, _, err := manager.GenerateToken("user@example.com", "User")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	svc := NewAuthService(manager, nil, database)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		user, ok := GetAuthUser(r.Context())
		if !ok {
			t.Fatalf("expected auth user in context")
		}
		if user.Email != "user@example.com" {
			t.Fatalf("unexpected user email: %s", user.Email)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/.gateway/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rw := httptest.NewRecorder()

	svc.Middleware(next).ServeHTTP(rw, req)

	if !nextCalled {
		t.Fatalf("next handler was not called; auth may have failed")
	}
	if rw.Code == http.StatusUnauthorized {
		t.Fatalf("expected successful auth, got 401")
	}
}
