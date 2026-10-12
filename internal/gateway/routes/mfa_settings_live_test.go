package routes_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// The CLI proxy refuses a user who must enroll in MFA but hasn't, whatever channel their session came from: a web
// session's token can be sent as a Bearer token too.
func TestUnenrolledWebSessionCantUseTheCLIProxy(t *testing.T) {
	e := newMFARouteEnv(t, "")
	user, err := e.database.CreateUser("new@example.com", "New User", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	s := e.newSession(t, user)

	w := e.do("GET", "/api/v1/rack-proxy/apps", "", bearer(s))
	assertStatus(t, w, http.StatusForbidden, "rack-proxy")
	if !strings.Contains(w.Body.String(), "mfa_enrollment_required") {
		t.Fatalf("rack-proxy: expected mfa_enrollment_required, got %s", w.Body.String())
	}
}

// MFA settings are read for each request, so a change applies without a restart.
func TestMFASettingsChangesApplyAtOnce(t *testing.T) {
	e := newMFARouteEnv(t, "")
	user, err := e.database.CreateUser("new@example.com", "New User", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	s := e.newSession(t, user)

	assertStatus(t, e.do("GET", "/api/v1/users", "", s.headers), http.StatusForbidden, "enrollment required")

	svc := settings.NewService(e.database)
	if err := svc.SetMFASettings(&db.MFASettings{RequireAllUsers: false}, nil); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, e.do("GET", "/api/v1/users", "", s.headers), http.StatusOK, "MFA no longer required")
	assertStatus(t, e.do("GET", "/api/v1/rack-proxy/apps", "", bearer(s)), http.StatusOK, "rack-proxy")
}

// Revealing secret env values needs a recent step-up; reading the masked values doesn't.
func TestRevealingSecretsNeedsAStepUp(t *testing.T) {
	e := newMFARouteEnv(t, "reveal@example.com")
	s := e.newSession(t, e.user)
	e.markVerified(t, s, false)

	w := e.do("GET", "/api/v1/apps/web/env?secrets=true", "", s.headers)
	assertStatus(t, w, http.StatusUnauthorized, "reveal without a step-up")
	if !strings.Contains(w.Body.String(), "mfa_step_up_required") {
		t.Fatalf("reveal: expected mfa_step_up_required, got %s", w.Body.String())
	}

	if w := e.do("GET", "/api/v1/apps/web/env", "", s.headers); w.Code == http.StatusUnauthorized {
		t.Fatalf("masked read: expected no MFA prompt, got %s", w.Body.String())
	}

	headers := withTOTP(s.headers, e.currentCode(t))
	if w := e.do("GET", "/api/v1/apps/web/env?secrets=true", "", headers); w.Code == http.StatusUnauthorized {
		t.Fatalf("reveal with an inline code: expected no MFA prompt, got %s", w.Body.String())
	}
}
